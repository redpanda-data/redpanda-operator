// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package genpartial loads up a go package and recursively generates a
// "partial" variant of an specified struct.
//
// If you've ever worked with the AWS SDK, you've worked with "partial" structs
// before. They are any struct where every field is nullable and the json tag
// specifies "omitempty".
//
// genpartial allows us to write structs in ergonomic go where fields that must
// always exist are presented as values rather than pointers. In cases we were
// need to marshal a partial value back to json or only specify a subset of
// values (IE helm values), use the generated partial.
package partial

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
	gofumpt "mvdan.cc/gofumpt/format"
)

const (
	mode = packages.NeedTypes | packages.NeedName | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports
)

type partialImport struct {
	Name string
	Path string
}

var packagePartials = map[string]partialImport{
	"k8s.io/api/core/v1": {
		Name: "applycorev1",
		Path: "k8s.io/client-go/applyconfigurations/core/v1",
	},
}

func Cmd() *cobra.Command {
	var outFlag string
	var headerFlag string
	var structFlag string
	var outPackage string

	cmd := &cobra.Command{
		Use:     "partial [pkg]",
		Args:    cobra.ExactArgs(1),
		Example: "partial [--header ./path/to/license] [--package redpanda] --struct Values ./charts/redpanda",
		Run: func(cmd *cobra.Command, args []string) {
			run(args, outFlag, outPackage, headerFlag, structFlag)
		},
	}

	cmd.Flags().StringVar(&outFlag, "out", "-", "The file to output to or `-` for stdout")
	cmd.Flags().StringVar(&outPackage, "package", "", "The package name to use for the output file. Defaults to the input package.")
	cmd.Flags().StringVar(&headerFlag, "header", "", "A file that will be used as a header for the generated file")
	cmd.Flags().StringVar(&structFlag, "struct", "Values", "The struct name to generate a partial for")

	return cmd
}

func run(args []string, outFlag, outPackage, headerFlag, structFlag string) {
	cwd, _ := os.Getwd()

	pkgs := Must(packages.Load(&packages.Config{
		Dir:  cwd,
		Mode: mode,
	}, args[0]))

	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			panic(err)
		}
	}

	var buf bytes.Buffer

	if headerFlag != "" {
		header, err := os.ReadFile(headerFlag)
		if err != nil {
			panic(err)
		}
		_, _ = buf.Write(header)
	}

	if len(pkgs) == 0 {
		panic(errors.Newf("failed to import package: %q from directory %q", args[0], cwd))
	}

	if err := GeneratePartial(pkgs[0], structFlag, outPackage, &buf); err != nil {
		panic(err)
	}

	if outFlag == "-" {
		fmt.Println(buf.String())
	} else {
		if err := os.WriteFile(outFlag, buf.Bytes(), 0o644); err != nil {
			panic(err)
		}
	}
}

type Generator struct {
	pkg   *packages.Package
	cache map[types.Type]ast.Expr
}

func (g *Generator) Generate(t types.Type) []ast.Node {
	toPartialize := FindAllNames(g.pkg.Types, t)

	var out []ast.Node
	for _, named := range toPartialize {
		// For any types that we've identified as wanting to partialize,
		// generate a new anonymous struct from the underlying struct of the
		// named type.
		// This allows the partialization algorithm to be much more sane and
		// terse. Partialization of named types is a game of deciding if the
		// reference needs to be a pointer or changed to a newly generated
		// type. Partialization of (anonymous) structs, is generation of a new
		// struct type.
		partialized := g.partialize(named.Underlying(), nil)

		var params *ast.FieldList
		if named.TypeParams().Len() > 0 {
			params = &ast.FieldList{List: make([]*ast.Field, named.TypeParams().Len())}
			for i := 0; i < named.TypeParams().Len(); i++ {
				param := named.TypeParams().At(i)
				params.List[i] = &ast.Field{
					Names: []*ast.Ident{{Name: param.Obj().Name()}},
					Type:  g.typeToNode(param.Constraint()).(ast.Expr),
				}
			}
		}

		out = append(out, &ast.DeclStmt{
			Decl: &ast.GenDecl{
				Tok: token.TYPE,
				Specs: []ast.Spec{
					&ast.TypeSpec{
						Name:       &ast.Ident{Name: g.partialName(named.Obj().Name())},
						TypeParams: params,
						Type:       g.typeToNode(partialized).(ast.Expr),
					},
				},
			},
		})
	}

	return out
}

func (g *Generator) qualifier(p *types.Package) string {
	if g.pkg.Types == p {
		return "" // same package; unqualified
	}

	// Technically this could break in the case of having multiple files
	// with different import aliases.
	for _, obj := range g.pkg.TypesInfo.Defs {
		if name, ok := obj.(*types.PkgName); ok && p.Path() == name.Imported().Path() {
			return name.Name()
		}
	}

	// If no package name was found in Defs, there's no import alias.
	// Fallback to p.Name().
	return p.Name()
}

func (g *Generator) typeToNode(t types.Type) ast.Node {
	str := types.TypeString(t, g.qualifier)
	node, err := parser.ParseExpr(str)
	if err != nil {
		panic(fmt.Errorf("%s\n%v", str, err))
	}
	return node
}

func (g *Generator) partialize(t types.Type, tag *StructTag) types.Type {
	// TODO cache me.

	switch t := t.(type) {
	case *types.Basic, *types.Interface, *types.Alias:
		return t
	case *types.Pointer:
		return types.NewPointer(g.partialize(t.Elem(), tag))
	case *types.Map:
		return types.NewMap(t.Key(), g.partialize(t.Elem(), nil))
	case *types.Slice:
		return types.NewSlice(g.partialize(t.Elem(), tag))
	case *types.Struct:
		return g.partializeStruct(t)
	case *types.Named:
		return g.partializeNamed(t, tag)
	case *types.TypeParam:
		return t // TODO this isn't super easy to fully support without a lot of additional information......
	default:
		panic(fmt.Sprintf("Unhandled: %T", t))
	}
}

func (g *Generator) partializeStruct(t *types.Struct) *types.Struct {
	tags := make([]string, t.NumFields())
	fields := make([]*types.Var, t.NumFields())
	for i := 0; i < t.NumFields(); i++ {
		field := t.Field(i)

		partialized := g.partialize(field.Type(), parseTag(t.Tag(i)).Named("partial"))
		switch partialized.Underlying().(type) {
		case *types.Basic:
			partialized = types.NewPointer(partialized)
		case *types.Struct:
			// Embedding of pointer values is kinda weird, so we don't do
			// it. TODO: this should technically only be done if no JSON tag
			// is explicitly specified (e.g. ObjectMeta) but we don't currently have any such
			// cases.
			if !field.Embedded() {
				partialized = types.NewPointer(partialized)
			}
		}

		// TODO Docs injection would be nice but given that we're crawling the
		// type tree, that's going to be quite difficult. Could probably stash
		// away a map of types to comments and then inject that into the ast
		// after parsing?
		// Or just implement our own type printer.
		tags[i] = EnsureOmitEmpty(t.Tag(i))
		fields[i] = types.NewField(0, g.pkg.Types, field.Name(), partialized, field.Embedded())
	}

	return types.NewStruct(fields, tags)
}

func (g *Generator) partializeNamed(t *types.Named, tag *StructTag) types.Type {
	// If there exists a Partial___ variant of the type, we'll use this. This
	// allows Partial structs to references partial structs from other packages
	// that contain Partialized structs and/or allows end users to provide
	// "manual" implementations of certain types.
	inPkg := t.Obj().Pkg() == g.pkg.Types
	partialName := g.partialName(t.Obj().Name())
	if obj := t.Obj().Pkg().Scope().Lookup(partialName); obj != nil {
		// Normally, we'd rely on build flags here but redpanda's values rely
		// on console's PartialValues. Instead, we check if the file looks like
		// a generated files as this will always resolve to the results of the
		// previous generation for redpanda.
		// To be a bit more accurate, we could alternatively get the full
		// source file and manually check for the go:build constraint.
		srcFile := g.pkg.Fset.Position(obj.Pos()).Filename
		if !inPkg || !strings.HasSuffix(srcFile, ".gen.go") {
			return obj.Type()
		}
	}

	// This check isn't going to be correct in the long run but it's intention
	// boils down to "Have we generated a Partialized version of this named
	// type?"
	// NB: This check MUST match the check in FindAllNames.
	isPartialized := inPkg && !IsType[*types.Basic](t.Underlying())
	if !isPartialized {
		if tag != nil {
			for _, value := range tag.Values {
				// "builtin" is used for types that have pre-defined partialized
				// variants, such as types that have pre-generated
				// k8s.io/client-go/applyconfigurations types.
				if value == "builtin" {
					path := t.Obj().Pkg().Path()
					if override, ok := packagePartials[path]; ok {
						var args []types.Type
						for i := 0; i < t.TypeArgs().Len(); i++ {
							args = append(args, g.partialize(t.TypeArgs().At(i), nil))
						}

						params := make([]*types.TypeParam, t.TypeParams().Len())
						for i := 0; i < t.TypeParams().Len(); i++ {
							param := t.TypeParams().At(i)
							// Might need to clone the typename here
							params[i] = types.NewTypeParam(param.Obj(), param.Constraint())
						}

						named := types.NewNamed(types.NewTypeName(0, types.NewPackage(override.Path, override.Name), t.Obj().Name()+"ApplyConfiguration", t.Underlying()), t.Underlying(), nil)
						if len(args) < 1 {
							return named
						}
						named.SetTypeParams(params)
						result, err := types.Instantiate(nil, named, args, true)
						if err != nil {
							panic(err)
						}
						return result
					}
				}
			}
		}

		// If we haven't partialized this type, there's nothing we can do. Noop.
		return t
	}

	// If this is a partialized type, we just need to make a NamedTyped with
	// any type params that reference the partial name. The Underlying aspect
	// of this named type is ignored so we pass in the existing underlying type
	// as nil isn't acceptable.

	var args []types.Type
	for i := 0; i < t.TypeArgs().Len(); i++ {
		args = append(args, g.partialize(t.TypeArgs().At(i), nil))
	}

	params := make([]*types.TypeParam, t.TypeParams().Len())
	for i := 0; i < t.TypeParams().Len(); i++ {
		param := t.TypeParams().At(i)
		// Might need to clone the typename here
		params[i] = types.NewTypeParam(param.Obj(), param.Constraint())
	}

	named := types.NewNamed(types.NewTypeName(0, g.pkg.Types, partialName, t.Underlying()), t.Underlying(), nil)
	if len(args) < 1 {
		return named
	}
	named.SetTypeParams(params)
	result, err := types.Instantiate(nil, named, args, true)
	if err != nil {
		panic(err)
	}
	return result
}

func (g *Generator) partialName(name string) string {
	return "Partial" + name
}

func GeneratePartial(pkg *packages.Package, structName string, outPackage string, out io.Writer) error {
	root := pkg.Types.Scope().Lookup(structName)

	if root == nil {
		return errors.Newf("named struct not found in package %q: %q", pkg.Name, structName)
	}

	if !IsType[*types.Named](root.Type()) || !IsType[*types.Struct](root.Type().Underlying()) {
		return errors.Newf("named struct not found in package %q: %q", pkg.Name, structName)
	}

	if outPackage == "" {
		outPackage = pkg.Name
	}

	gen := Generator{pkg: pkg, cache: map[types.Type]ast.Expr{}}

	partials := gen.Generate(root.Type())

	// Map of import aliases to actual package for all imports in the
	// originally parsed package.
	// One of the more frustrating aspects of Go's AST/Type system is dealing
	// with imports and their aliases. The best way to get them is to crawl the
	// AST for import specs and then manually resolve them.
	originalImports := map[string]*types.Package{}
	for _, f := range pkg.Syntax {
		for _, imp := range f.Imports {
			// For some reason, imports can be nil.
			if imp == nil {
				continue
			}

			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				panic(err)
			}

			imported := pkg.Imports[path]
			name := imported.Name

			// if an alias is specified, use it.
			if imp.Name != nil {
				name = imp.Name.Name
			}

			originalImports[name] = imported.Types
		}
	}

	// Now that we have our partial structs, we need to generate the import
	// block for them. We'll crawl the AST of the structs and find references
	// to external packages. This method could possibly lead to conflicts as
	// we're just looking for [ast.SelectorExpr]'s
	imports := map[string]*types.Package{}
	for _, partial := range partials {
		ast.Inspect(partial, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				parent, ok := n.X.(*ast.Ident)
				if !ok {
					return true
				}

				if pkg, ok := originalImports[parent.Name]; ok {
					imports[parent.Name] = pkg
				}

				for _, pkg := range packagePartials {
					if pkg.Name == parent.Name {
						// NB: we don't actually use the import name below, so
						// we just set it to empty here
						imports[pkg.Name] = types.NewPackage(pkg.Path, "")
					}
				}
			}
			return true
		})
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "//go:build !generate\n\n")
	fmt.Fprintf(&buf, "// +gotohelm:ignore=true\n")
	fmt.Fprintf(&buf, "//\n")
	// This line must match `^// Code generated .* DO NOT EDIT\.$`. See https://pkg.go.dev/cmd/go#hdr-Generate_Go_files_by_processing_source
	fmt.Fprintf(&buf, "// Code generated by genpartial DO NOT EDIT.\n")
	fmt.Fprintf(&buf, "package %s\n\n", outPackage)

	// Only print out imports if we have them. We lean on source.Format later
	// to align and sort them.
	if len(imports) > 0 {
		fmt.Fprintf(&buf, "import (\n")
		for name, pkg := range imports {
			if pkg.Name() == name {
				fmt.Fprintf(&buf, "\t%q\n", pkg.Path())
			} else {
				fmt.Fprintf(&buf, "\t%s %q\n", name, pkg.Path())
			}
		}
		fmt.Fprintf(&buf, ")\n\n")
	}

	for i, d := range partials {
		if i > 0 {
			fmt.Fprintf(&buf, "\n\n")
		}
		err := format.Node(&buf, token.NewFileSet(), d)
		if err != nil {
			return err
		}
	}

	formatted, err := gofumpt.Source(buf.Bytes(), gofumpt.Options{})
	if err != nil {
		return err
	}

	_, err = out.Write(formatted)
	return err
}

// FindAllNames traverses the given type and returns a slice of all non-Basic
// named types that are referenced from the "root" type.
func FindAllNames(pkg *types.Package, root types.Type) []*types.Named {
	names := []*types.Named{}
	seen := map[types.Type]struct{}{}
	toTraverse := []types.Type{}

	push := func(t types.Type) {
		if _, ok := seen[t]; ok {
			return
		}

		// Partialize all named types within this the provided package that are
		// not aliases for Basic types.
		// This could be "more efficient" by avoiding partialization of named
		// types that don't require changes but that's much more error prone
		// and makes working with partialized types a bit strange.
		if named, ok := t.(*types.Named); ok && named.Obj().Pkg() == pkg && named.Origin() == named {
			switch named.Underlying().(type) {
			case *types.Basic:
			default:
				names = append(names, named)
			}
		}

		seen[t] = struct{}{}
		toTraverse = append(toTraverse, t)
	}

	push(root)

	for len(toTraverse) > 0 {
		current := toTraverse[0]
		toTraverse = toTraverse[1:]

		push(current.Underlying())

		switch current := current.(type) {
		case *types.Basic, *types.Interface, *types.TypeParam, *types.Alias:
			continue

		case *types.Pointer:
			push(current.Elem())

		case *types.Array:
			push(current.Elem())

		case *types.Slice:
			push(current.Elem())

		case *types.Map:
			push(current.Key())
			push(current.Elem())

		case *types.Struct:
			for i := 0; i < current.NumFields(); i++ {
				push(current.Field(i).Type())
			}
		case *types.Named:
			push(current.Origin())
			for i := 0; i < current.TypeArgs().Len(); i++ {
				push(current.TypeArgs().At(i))
			}

		default:
			panic(fmt.Sprintf("unhandled: %T", current))
		}
	}

	return names
}

// EnsureOmitEmpty injects ,omitempty into existing json tags or adds one if
// not already present. As a special case, if not json tag is present but yaml
// is, the yaml tag will be used.
func EnsureOmitEmpty(tag string) string {
	parts := parseTag(tag)

	yamlName := ""
	for _, part := range parts {
		if part.Name == "yaml" && len(part.Values) > 0 {
			yamlName = part.Values[0]
			break
		}
	}

	idx := slices.IndexFunc(parts, func(p StructTag) bool {
		return p.Name == "json"
	})

	if idx == -1 {
		idx = len(parts)
		parts = append(parts, StructTag{Name: "json"})
	}

	jsonTag := &parts[idx]

	if len(jsonTag.Values) == 0 {
		jsonTag.Values = append(jsonTag.Values, yamlName)
	} else if jsonTag.Values[0] == "" { // e.g. `yaml:"name" json:",omitempty"`
		jsonTag.Values[0] = yamlName
	}

	if !slices.Contains(jsonTag.Values, "omitempty") {
		jsonTag.Values = append(jsonTag.Values, "omitempty")
	}

	var out strings.Builder
	for i, p := range parts {
		if p.Name == "partial" {
			continue
		}
		if i > 0 {
			_, _ = out.WriteRune(' ')
		}
		out.WriteString(p.String())
	}

	return out.String()
}

func Must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func IsType[T types.Type](typ types.Type) bool {
	_, ok := typ.(T)
	return ok
}

var tagRe = regexp.MustCompile(`([a-z_]+):"([^"]+)"`)

type StructTags []StructTag

func (t StructTags) Named(name string) *StructTag {
	for i, tag := range t {
		if tag.Name == name {
			return &t[i]
		}
	}
	return nil
}

func parseTag(tag string) StructTags {
	matches := tagRe.FindAllStringSubmatch(tag, -1)

	tags := make([]StructTag, len(matches))
	for i, match := range matches {
		tags[i] = StructTag{
			Name:   match[1],
			Values: strings.Split(match[2], ","),
		}
	}

	return tags
}

type StructTag struct {
	Name   string
	Values []string
}

func (t *StructTag) String() string {
	return fmt.Sprintf(`%s:"%s"`, t.Name, strings.Join(t.Values, ","))
}
