// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:gosec // this is a code generation tool
package applyconfigcopy

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/packages"
)

var (
	loadConfig = &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes,
	}

	// we only care about certain packages, if an import is not in this list we can ignore it
	knownPackagePrefixes = []string{
		"k8s.io/apimachinery/pkg/api",
		"k8s.io/apimachinery/pkg/util/intstr",
		"k8s.io/api/core/v1",
		"k8s.io/client-go/applyconfigurations",
	}
)

func Cmd() *cobra.Command {
	var outFlag string
	var headerFlag string
	var structFlag string
	var outPackage string
	var testFlag string

	cmd := &cobra.Command{
		Use:     "applyconfig-copy [pkg]",
		Args:    cobra.ExactArgs(1),
		Example: "applyconfig-copy [--header ./path/to/license] [--package redpanda] --struct Values ./charts/redpanda",
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(args[0], outFlag, outPackage, headerFlag, structFlag, testFlag); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&outFlag, "out", "-", "The file to output to or `-` for stdout")
	cmd.Flags().StringVar(&outPackage, "package", "", "The package name to use for the output file. Defaults to the input package.")
	cmd.Flags().StringVar(&headerFlag, "header", "", "A file that will be used as a header for the generated file")
	cmd.Flags().StringVar(&structFlag, "struct", "", "The struct name to generate a partial for")
	cmd.Flags().StringVar(&testFlag, "test", "", "The output location for a test of the DeepCopy function for the specified struct")

	return cmd
}

func run(inputPackagePath, outputFile, outputPackage, headerFile, structureName, testFile string) error {
	if structureName == "" {
		return errors.New("--struct is required")
	}

	parser, err := Parse(inputPackagePath, outputPackage, structureName)
	if err != nil {
		return fmt.Errorf("Error parsing package: %w", err)
	}

	var header string
	if headerFile != "" {
		headerBytes, err := os.ReadFile(headerFile)
		if err != nil {
			return fmt.Errorf("Error reading header file: %w", err)
		}
		header = string(headerBytes) + "\n"
	}

	generator := parser.Generator()
	code, err := generator.Generate()
	if err != nil {
		return fmt.Errorf("Error generating DeepCopy code: %w", err)
	}

	if testFile != "" {
		testCode, err := generator.GenerateTest()
		if err != nil {
			return fmt.Errorf("Error generating DeepCopy test code: %w", err)
		}

		err = os.WriteFile(testFile, []byte(header+testCode), 0o644)
		if err != nil {
			return fmt.Errorf("Error writing test file: %w", err)
		}
	}

	if outputFile == "-" {
		if _, err := fmt.Println(header + code); err != nil {
			return fmt.Errorf("Error writing to stdout: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(outputFile, []byte(header+code), 0o644); err != nil {
		return fmt.Errorf("Error writing output file: %w", err)
	}
	return nil
}

type Parser struct {
	inputPackage     string
	inputPackagePath string
	packageName      string
	structName       string
	structType       *ast.StructType
	processed        map[string]struct{}
	imports          map[string]string
	structTypes      map[structIdentifier]packageStruct
}

func Parse(inputPackagePath, packageName, structName string) (*Parser, error) {
	pkgs, err := packages.Load(loadConfig, inputPackagePath)
	if err != nil {
		return nil, err
	}

	if len(pkgs) != 1 {
		return nil, fmt.Errorf("expected exactly one package, found %d", len(pkgs))
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		var err error
		for _, pkgErr := range pkg.Errors {
			err = errors.Join(err, pkgErr)
		}
		return nil, err
	}

	parts := strings.Split(pkg.PkgPath, "/")
	inputPackage := strings.Join(parts[len(parts)-2:], "") + "ac"

	p := &Parser{
		inputPackage:     inputPackage,
		inputPackagePath: inputPackagePath,
		packageName:      packageName,
		structName:       structName,
		processed:        make(map[string]struct{}),
		imports:          make(map[string]string),
		structTypes:      make(map[structIdentifier]packageStruct),
	}

	if err := p.collect(pkg.PkgPath); err != nil {
		return nil, err
	}

	structType, ok := p.structTypes[structIdentifier{packagePath: pkg.PkgPath, structName: structName}]
	if !ok {
		return nil, fmt.Errorf("Struct %s not found in package %s\n", structName, pkg.PkgPath)
	}
	p.structType = structType.structType

	return p, nil
}

func (p *Parser) collect(basePackage string) error {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes,
	}, basePackage)
	if err != nil {
		return err
	}

	for _, pkg := range pkgs {
		if _, ok := p.processed[pkg.PkgPath]; ok {
			continue
		}

		if err := p.collectStructTypes(pkg); err != nil {
			return err
		}
		if err := p.collectImports(pkg); err != nil {
			return err
		}
		p.processed[pkg.PkgPath] = struct{}{}
	}

	for _, importName := range p.imports {
		if _, ok := p.processed[importName]; ok {
			continue
		}
		if err := p.collect(importName); err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) collectImports(pkg *packages.Package) error {
	if _, ok := p.processed[pkg.PkgPath]; ok {
		return nil
	}

	for _, file := range pkg.Syntax {
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")

			if !slices.ContainsFunc(knownPackagePrefixes, func(prefix string) bool {
				return strings.HasPrefix(importPath, prefix)
			}) {
				continue
			}

			alias := normalizeOrConstructAlias(imp.Name, importPath)
			if existingPath, exists := p.imports[alias]; exists && existingPath != importPath {
				return fmt.Errorf("import alias conflict: %s maps to both %s and %s", alias, existingPath, importPath)
			}
			p.imports[alias] = importPath
			p.imports[importPath] = alias
		}
	}

	return nil
}

type structIdentifier struct {
	packagePath string
	structName  string
}

type packageStruct struct {
	packagePath string
	structType  *ast.StructType
}

func (p *Parser) collectStructTypes(pkg *packages.Package) error {
	if _, ok := p.processed[pkg.PkgPath]; ok {
		return nil
	}

	var err error
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok {
				if st, ok := ts.Type.(*ast.StructType); ok {
					name := ts.Name.Name
					if old, exists := p.structTypes[structIdentifier{packagePath: pkg.PkgPath, structName: name}]; exists {
						err = fmt.Errorf("struct type conflict: %s defined in multiple packages: %q and %q", name, old.packagePath, pkg.PkgPath)
						return false
					}
					p.structTypes[structIdentifier{packagePath: pkg.PkgPath, structName: name}] = packageStruct{
						packagePath: pkg.PkgPath,
						structType:  st,
					}
				}
			}
			return true
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) Generator() *Generator {
	return &Generator{
		packagePrefix:    p.inputPackage + ".",
		packageName:      p.packageName,
		structName:       p.structName,
		inputPackage:     p.inputPackage,
		inputPackagePath: p.inputPackagePath,
		imports:          p.imports,
		structType:       p.structType,
		structTypes:      p.structTypes,
		usedImports: map[string]string{
			p.inputPackage: p.inputPackagePath,
		},
	}
}

type Generator struct {
	packagePrefix    string
	packageName      string
	inputPackage     string
	inputPackagePath string
	structName       string
	structType       *ast.StructType
	imports          map[string]string
	structTypes      map[structIdentifier]packageStruct
	usedImports      map[string]string
}

func (g *Generator) GenerateTest() (string, error) {
	var buf strings.Builder
	if err := testTemplate.Execute(&buf, testTemplateData{
		Package:    g.packageName,
		Import:     fileImport{Alias: g.inputPackage, Path: g.inputPackagePath},
		StructName: g.structName,
	}); err != nil {
		return "", err
	}

	src := buf.String()
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return "", err
	}

	return string(formatted), nil
}

func (g *Generator) Generate() (string, error) {
	deepCopyCode, err := g.generateDeepCopy()
	if err != nil {
		return "", err
	}

	importsList := []fileImport{}
	for alias, path := range g.usedImports {
		importsList = append(importsList, fileImport{Alias: alias, Path: path})
	}

	var buf strings.Builder
	if err := fileTemplate.Execute(&buf, fileTemplateData{
		Package:      g.packageName,
		Imports:      importsList,
		DeepCopyCode: deepCopyCode,
	}); err != nil {
		return "", err
	}

	src := buf.String()
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

func (g *Generator) generateDeepCopy() (string, error) {
	var fieldCopies strings.Builder
	for _, field := range g.structType.Fields.List {
		if field.Names == nil {
			typeName := ""
			switch t := field.Type.(type) {
			case *ast.Ident:
				typeName = t.Name
			case *ast.SelectorExpr:
				typeName = t.Sel.Name
			}
			if typeName != "" {
				if err := g.generateCopy(&fieldCopies, fmt.Sprintf("in.%s", typeName), fmt.Sprintf("out.%s", typeName), "", field.Type, false); err != nil {
					return "", err
				}
			}
			continue
		}
		for _, name := range field.Names {
			fieldName := name.Name
			if err := g.generateCopy(&fieldCopies, fmt.Sprintf("in.%s", fieldName), fmt.Sprintf("out.%s", fieldName), "", field.Type, false); err != nil {
				return "", err
			}
		}
	}

	var buf strings.Builder
	if err := deepCopyTemplate.Execute(&buf, deepCopyTemplateData{
		StructName:    g.structName,
		PackagePrefix: g.packagePrefix,
		FieldCopies:   fieldCopies.String(),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *Generator) generateCopy(buf *strings.Builder, inPath, outPath, knownPkg string, expr ast.Expr, skipInitial bool) error {
	switch t := expr.(type) {
	case *ast.Ident:
		if err := g.handleIdentCopy(buf, t, inPath, outPath, knownPkg, skipInitial); err != nil {
			return err
		}
	case *ast.SelectorExpr:
		if err := g.handleSelectorExprCopy(buf, t, inPath, outPath, knownPkg, skipInitial); err != nil {
			return err
		}
	case *ast.ArrayType:
		if err := g.handleArrayCopy(buf, t, inPath, outPath, knownPkg); err != nil {
			return err
		}
	case *ast.MapType:
		if err := g.handleMapCopy(buf, t, inPath, outPath, knownPkg); err != nil {
			return err
		}
	case *ast.StarExpr:
		if err := g.handleStarExprCopy(buf, t, inPath, outPath); err != nil {
			return err
		}
	default:
		if !skipInitial {
			if err := g.handleDefaultCopy(buf, inPath, outPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *Generator) handleIdentCopy(buf *strings.Builder, t *ast.Ident, inPath, outPath, knownPkg string, skipInitial bool) error {
	packagePath := g.inputPackagePath
	if knownPkg != "" {
		packagePath = g.imports[knownPkg]
	}

	if st, ok := g.structTypes[structIdentifier{packagePath: packagePath, structName: t.Name}]; ok {
		var fieldCopies strings.Builder
		for _, field := range st.structType.Fields.List {
			if field.Names == nil {
				if fieldName := getEmbeddedFieldName(field); fieldName != "" {
					if err := g.handleFieldCopy(&fieldCopies, field, fieldName, inPath, outPath, knownPkg); err != nil {
						return err
					}
				}
				continue
			}
			for _, name := range field.Names {
				if err := g.handleFieldCopy(&fieldCopies, field, name.Name, inPath, outPath, knownPkg); err != nil {
					return err
				}
			}
		}
		if !skipInitial {
			if err := structTemplate.Execute(buf, copyTemplateData{
				OutPath:       outPath,
				PackagePrefix: g.packagePrefix,
				TypeString:    t.Name,
			}); err != nil {
				return err
			}
		}
		_, err := buf.WriteString(fieldCopies.String())
		return err
	}

	name := t.Name
	if knownPkg != "" {
		name = knownPkg
	}
	if path, ok := g.imports[name]; ok && path != "" {
		g.usedImports[name] = path
	}
	return basicTemplate.Execute(buf, copyTemplateData{
		OutPath: outPath,
		InPath:  inPath,
	})
}

func (g *Generator) handleFieldCopy(buf *strings.Builder, t *ast.Field, fieldName, inPath, outPath, knownPkg string) error {
	subIn := fmt.Sprintf("(%s).%s", inPath, fieldName)
	subOut := fmt.Sprintf("(%s).%s", outPath, fieldName)
	if _, err := buf.WriteString("\n"); err != nil {
		return err
	}
	return g.generateCopy(buf, subIn, subOut, knownPkg, t.Type, false)
}

func (g *Generator) handleSelectorExprCopy(buf *strings.Builder, t *ast.SelectorExpr, inPath, outPath, knownPkg string, skipInitial bool) error {
	if !skipInitial {
		if err := selectorTemplate.Execute(buf, copyTemplateData{
			OutPath:      outPath,
			Alias:        knownPkg,
			SelectorName: t.Sel.Name,
		}); err != nil {
			return err
		}
	}

	return g.generateCopy(buf, inPath, outPath, knownPkg, t.X, false)
}

func (g *Generator) handleArrayCopy(buf *strings.Builder, t *ast.ArrayType, inPath, outPath, knownPkg string) error {
	var elemCopy string
	if g.isStructType(t.Elt, knownPkg) {
		var copyElements strings.Builder
		if _, err := copyElements.WriteString(fmt.Sprintf("in, out := &%s[i], &%s[i]\n", inPath, outPath)); err != nil {
			return err
		}
		if err := g.generateCopy(&copyElements, "in", "out", knownPkg, t.Elt, true); err != nil {
			return err
		}
		elemCopy = copyElements.String()
	}

	return arrayTemplate.Execute(buf, copyTemplateData{
		InPath:     inPath,
		OutPath:    outPath,
		TypeString: g.typeString(t, g.packagePrefix),
		IsStruct:   g.isStructType(t.Elt, knownPkg),
		ElemCopy:   elemCopy,
	})
}

func (g *Generator) handleMapCopy(buf *strings.Builder, t *ast.MapType, inPath, outPath, knownPkg string) error {
	var elemCopy string
	isStruct := g.isStructType(t.Value, knownPkg)
	if isStruct {
		var copyElements strings.Builder
		if _, err := copyElements.WriteString(fmt.Sprintf("in, out := &v, &%s[k]\n", outPath)); err != nil {
			return err
		}
		if err := g.generateCopy(&copyElements, "in", "out", knownPkg, t.Value, true); err != nil {
			return err
		}
		elemCopy = copyElements.String()
	}

	return mapTemplate.Execute(buf, copyTemplateData{
		InPath: inPath, OutPath: outPath,
		TypeString: g.typeString(t, g.packagePrefix),
		IsStruct:   isStruct,
		ElemCopy:   elemCopy,
	})
}

func (g *Generator) handleStarExprCopy(buf *strings.Builder, t *ast.StarExpr, inPath, outPath string) error {
	prefix := g.packagePrefix
	knownPkg := ""
	typeString := g.typeString(t.X, "")
	if strings.Contains(typeString, ".") {
		prefix = ""
		parts := strings.Split(typeString, ".")
		knownPkg = parts[0]
	}

	var elemCopy string
	isStruct := g.isStructType(t.X, knownPkg)
	if isStruct {
		var copyElements strings.Builder
		if err := g.generateCopy(&copyElements, fmt.Sprintf("*%s", inPath), fmt.Sprintf("*%s", outPath), knownPkg, t.X, true); err != nil {
			return err
		}
		elemCopy = copyElements.String()
	}

	return starTemplate.Execute(buf, copyTemplateData{
		InPath:        inPath,
		OutPath:       outPath,
		TypeString:    typeString,
		PackagePrefix: prefix,
		IsStruct:      isStruct,
		ElemCopy:      elemCopy,
	})
}

func (g *Generator) handleDefaultCopy(buf *strings.Builder, inPath, outPath string) error {
	return defaultTemplate.Execute(buf, copyTemplateData{
		OutPath: outPath,
		InPath:  inPath,
	})
}

func (g *Generator) isStructType(expr ast.Expr, knownPackage string) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		packagePath := g.inputPackagePath
		if knownPackage != "" {
			packagePath = g.imports[knownPackage]
		}
		_, ok := g.structTypes[structIdentifier{packagePath: packagePath, structName: t.Name}]
		return ok
	case *ast.StarExpr:
		knownPkg := ""
		typeString := g.typeString(t.X, "")
		if strings.Contains(typeString, ".") {
			parts := strings.Split(typeString, ".")
			knownPkg = parts[0]
		}
		return g.isStructType(t.X, knownPkg)
	case *ast.SelectorExpr:
		// Try to resolve qualified name for imported struct
		if ident, ok := t.X.(*ast.Ident); ok {
			_, ok := g.structTypes[structIdentifier{packagePath: g.imports[ident.Name], structName: t.Sel.Name}]
			return ok || strings.HasSuffix(t.Sel.Name, "ApplyConfiguration")
		}
		return false
	default:
		return false
	}
}

func (g *Generator) typeString(expr ast.Expr, prefix string) string {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64",
			"string", "bool",
			"byte", "rune":
			return t.Name
		default:
			return prefix + t.Name
		}
	case *ast.ArrayType:
		return "[]" + g.typeString(t.Elt, prefix)
	case *ast.MapType:
		return "map[" + g.typeString(t.Key, prefix) + "]" + g.typeString(t.Value, prefix)
	case *ast.StarExpr:
		return "*" + g.typeString(t.X, prefix)
	case *ast.SelectorExpr:
		alias := ""
		if ident, ok := t.X.(*ast.Ident); ok {
			name := normalizeAlias(ident.Name, t.Sel.Name)
			if path, ok := g.imports[name]; ok && path != "" {
				g.usedImports[name] = path
				alias = name
			} else {
				alias = name
			}
		}
		if alias != "" {
			return alias + "." + t.Sel.Name
		}
		return prefix + t.Sel.Name
	default:
		panic("unhandled type")
	}
}

func getEmbeddedFieldName(field *ast.Field) string {
	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.StarExpr:
		switch xt := t.X.(type) {
		case *ast.Ident:
			return xt.Name
		case *ast.SelectorExpr:
			return xt.Sel.Name
		}
	}
	return ""
}

func normalizeOrConstructAlias(packageName *ast.Ident, packagePath string) string {
	if packageName != nil {
		return normalizeAlias(packageName.Name, packagePath)
	}

	// Default alias is last element of import path, or if it ends with a number, last two elements
	parts := strings.Split(packagePath, "/")
	alias := parts[len(parts)-1]
	last := alias[len(alias)-1]
	if _, err := strconv.ParseInt(string(last), 10, 64); err == nil {
		alias = strings.Join(parts[len(parts)-2:], "")
	}
	return alias
}

func normalizeAlias(packageName, structOrPathName string) string {
	if strings.Contains(strings.ToLower(structOrPathName), "applyconfiguration") {
		return packageName + "ac"
	}
	return packageName
}
