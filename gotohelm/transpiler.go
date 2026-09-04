// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package gotohelm

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
	"k8s.io/client-go/kubernetes/scheme"
)

var directiveRE = regexp.MustCompile(`\+gotohelm:([\w\.-]+)=([\w\.-]+)`)

// DiagnosticsError is the error returned when a package fails to transpile.
//
// It renders every diagnostic rather than only the first, so a chart author
// sees the full set of problems in one pass. That's the point of reporting
// instead of panicking: the old Unsupported panic unwound the whole walk, so
// the second problem in a file only surfaced once the first was fixed.
type DiagnosticsError struct {
	Fset        *token.FileSet
	Diagnostics []analysis.Diagnostic
}

func (e *DiagnosticsError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%d untranspilable construct(s):", len(e.Diagnostics))

	for _, diagnostic := range e.Diagnostics {
		fmt.Fprintf(&b, "\n%s: %s", e.Fset.PositionFor(diagnostic.Pos, false), diagnostic.Message)
	}

	return b.String()
}

type Chart struct {
	Files []*File
}

// report records that node can't be transpiled and returns [Invalid] to stand
// in for whatever it should have produced.
//
// It exists so that a rule can bail out of one expression or statement without
// unwinding the whole walk: the enclosing statement is poisoned, the loop over
// the function's body carries on, and a single pass reports every problem in
// the package rather than only the first one it happened to reach.
func (t *Transpiler) report(node ast.Node, format string, args ...any) Node {
	t.diagnostics = append(t.diagnostics, analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: "unsupported",
		Message:  fmt.Sprintf(format, args...),
	})

	return &Invalid{}
}

func Transpile(pkgs []*packages.Package, deps ...string) (*Chart, error) {
	for _, pkg := range pkgs {
		deps = append(deps, pkg.PkgPath)
	}

	files, diagnostics, err := analyze(pkgs, deps)
	if err != nil {
		return nil, err
	}

	if len(diagnostics) > 0 {
		slices.SortStableFunc(diagnostics, func(a, b analysis.Diagnostic) int {
			return cmp.Compare(a.Pos, b.Pos)
		})

		return nil, &DiagnosticsError{Fset: pkgs[0].Fset, Diagnostics: diagnostics}
	}

	shims, err := transpileBootstrap()
	if err != nil {
		return nil, err
	}

	return &Chart{Files: append(files, shims)}, nil
}

// analyze transpiles pkgs by running [NewAnalyzer] over them through the
// standard go/analysis driver.
//
// Going through the driver rather than calling the transpiler directly is what
// gives it a single entry point. The transpiler needs the `+gotohelm:`
// directives of the packages it calls into, and the only way one pass can see
// another package's source is via facts, which the driver propagates in
// dependency order. See facts.go.
func analyze(pkgs []*packages.Package, deps []string) ([]*File, []analysis.Diagnostic, error) {
	graph, err := checker.Analyze([]*analysis.Analyzer{NewAnalyzer(deps...)}, pkgs, nil)
	if err != nil {
		return nil, nil, errors.WithStack(err)
	}

	var files []*File
	var diagnostics []analysis.Diagnostic

	for _, root := range graph.Roots {
		if root.Err != nil {
			return nil, nil, errors.WithStack(root.Err)
		}

		files = append(files, root.Result.([]*File)...)
		diagnostics = append(diagnostics, root.Diagnostics...)
	}

	return files, diagnostics, nil
}

// newTranspiler builds a Transpiler over the package a go/analysis pass was
// handed.
func newTranspiler(pass *analysis.Pass, chart map[string]struct{}) *Transpiler {
	dependencies := map[string]struct{}{pass.Pkg.Path(): {}}
	for path := range chart {
		dependencies[path] = struct{}{}
	}

	return &Transpiler{
		Fset:      pass.Fset,
		TypesInfo: pass.TypesInfo,
		Types:     pass.Pkg,
		Files:     pass.Files,

		pass:         pass,
		namespaces:   map[*types.Package]string{},
		dependencies: dependencies,
		names:        map[*types.Func]string{},
		builtins: map[string]string{
			"fmt.Sprintf":                "printf",
			"golang.org/x/exp/maps.Keys": "keys",
			"maps.Keys":                  "keys",
			"math.Floor":                 "floor",
			"sort.Strings":               "sortAlpha",
			"strings.ToLower":            "lower",
			"strings.ToUpper":            "upper",
		},
	}
}

type Transpiler struct {
	Fset      *token.FileSet
	Files     []*ast.File
	TypesInfo *types.Info
	Types     *types.Package

	// builtins is a pre-populated cache of function id (fmt.Printf,
	// github.com/my/pkg.Function) to an equivalent go template / sprig
	// builtin. Functions may add a +gotohelm:builtin=blah directive to declare
	// their builtin equivalent.
	builtins map[string]string
	// dependencies is a pre-populated set of fully qualified go package paths of permitted
	// function calls. It should contain the path of .Package and any dependent
	// charts (subcharts).
	dependencies map[string]struct{}
	// namespaces is a cache for holding the namespace package directive. It's
	// exclusively used by `namespaceFor`.
	namespaces map[*types.Package]string
	// names is a cache for holding the transpiled name of a function.
	// It's exclusively used by `funcNameFor`.
	names map[*types.Func]string

	// diagnostics collects everything report was handed.
	diagnostics []analysis.Diagnostic

	// pass is how the transpiler reaches its dependencies' directives, which
	// arrive as facts. See facts.go.
	pass *analysis.Pass
}

func (t *Transpiler) Transpile() []*File {
	var files []*File
	for _, f := range t.Files {
		if transpiled := t.transpileFile(f); transpiled != nil {
			files = append(files, transpiled)
		}
	}

	return files
}

func (t *Transpiler) transpileFile(f *ast.File) *File {
	path := t.Fset.File(f.Pos()).Name()
	source := filepath.Base(path)

	name := fmt.Sprintf("_%s.%s.tpl", t.namespaceFor(t.Types), source[:len(source)-3])

	isTestFile := strings.HasSuffix(name, "_test.go")
	if isTestFile || name == "main.go" {
		return nil
	}

	fileDirectives := parseDirectives(f.Doc.Text())
	if _, ok := fileDirectives["filename"]; ok {
		name = fileDirectives["filename"]
	}

	if _, ok := fileDirectives["ignore"]; ok {
		return nil
	}

	var funcs []*Func
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}

		funcDirectives := parseDirectives(fn.Doc.Text())
		if v, ok := funcDirectives["ignore"]; ok && v == "true" {
			continue
		}

		// Don't transpile functions that are stand ins for sprig / helm provided
		// functions. transpileExpr handles references to these functions by
		// reading this annotation.
		if _, ok := funcDirectives["builtin"]; ok {
			continue
		}

		var params []Node
		if fn.Recv != nil {
			for _, param := range fn.Recv.List {
				for _, name := range param.Names {
					params = append(params, t.transpileExpr(name))
				}
			}
		}

		for _, param := range fn.Type.Params.List {
			for _, name := range param.Names {
				params = append(params, t.transpileExpr(name))
			}
		}

		var statements []Node
		for _, stmt := range fn.Body.List {
			statements = append(statements, t.transpileStatement(stmt))
		}

		// TODO add a source field here? Ideally with a line number.
		funcs = append(funcs, &Func{
			Name:       t.funcNameFor(t.TypesInfo.ObjectOf(fn.Name).(*types.Func)),
			Namespace:  t.namespaceFor(t.Types),
			Params:     params,
			Statements: statements,
		})
	}

	return &File{
		Name:   name,
		Source: filepath.Join(t.Types.Path(), source),
		Funcs:  funcs,
	}
}

func (t *Transpiler) transpileStatement(stmt ast.Stmt) Node {
	switch stmt := stmt.(type) {
	case nil:
		return nil

	case *ast.DeclStmt:
		switch d := stmt.Decl.(type) {
		case *ast.GenDecl:
			if len(d.Specs) > 1 {
				// TODO could just return multiple statements.
				return t.report(d, "declarations may only contain 1 spec")
			}

			spec := d.Specs[0].(*ast.ValueSpec)

			if len(spec.Names) > 1 || len(spec.Values) > 1 {
				return t.report(d, "specs may only contain 1 value")
			}

			rhs := t.zeroOf(t.TypesInfo.TypeOf(spec.Names[0]))
			if len(spec.Values) > 0 {
				rhs = t.transpileExpr(spec.Values[0])
			}

			return &Assignment{
				LHS: t.transpileExpr(spec.Names[0]),
				New: true,
				RHS: rhs,
			}

		default:
			panic(fmt.Sprintf("unsupported declaration: %#v", d))
		}

	case *ast.BranchStmt:
		switch stmt.Tok {
		case token.BREAK:
			return &Statement{NoCapture: true, Expr: Literal("break")}

		case token.CONTINUE:
			return &Statement{NoCapture: true, Expr: Literal("continue")}
		}

	case *ast.ReturnStmt:
		if len(stmt.Results) == 1 {
			return &Return{Expr: t.transpileExpr(stmt.Results[0])}
		}

		var results []Node
		for _, r := range stmt.Results {
			results = append(results, t.transpileExpr(r))
		}
		return &Return{Expr: &BuiltInCall{Func: Literal("list"), Arguments: results}}

	case *ast.AssignStmt:
		// Multi-value assignments (x, y := 1, 2) are split into one assignment
		// per pair by
		// [github.com/redpanda-data/redpanda-operator/gotohelm/internal/rewrite.Rewrite]
		// before transpilation, which also handles the case where a naive
		// split would change the statement's meaning. Anything that survives
		// that would silently lose every pair but the first, so reject it.
		if len(stmt.Lhs) == len(stmt.Rhs) && len(stmt.Lhs) > 1 {
			return t.report(stmt, "multi-value assignment was not unrolled before transpilation")
		}

		if len(stmt.Lhs) > 1 && len(stmt.Rhs) == 1 {
			return t.transpileMVAssignStmt(stmt)
		}

		// Unhandled case, that may not be possible?
		// x, y, z := a, b
		if len(stmt.Lhs) != len(stmt.Rhs) {
			break
		}

		// +=, /=, *=, etc show up as assignments. They're not supported in
		// templates. We'll need to either rewrite the expression here or add
		// another AST rewrite.
		switch stmt.Tok {
		case token.ASSIGN, token.DEFINE:
		default:
			return t.report(stmt, "Unsupported assignment token")
		}

		// TODO could simplify this by performing a type switch on the
		// transpiled result of lhs.
		if _, ok := stmt.Lhs[0].(*ast.SelectorExpr); ok {
			var selector *Selector
			switch expr := t.transpileExpr(stmt.Lhs[0]).(type) {
			case *Selector:
				selector = expr
			case *Cast:
				selector = expr.X.(*Selector)
			default:
				panic(fmt.Sprintf("unhandled case %T: %v", expr, expr))
			}

			return &Statement{
				Expr: &BuiltInCall{
					Func: Literal("set"),
					Arguments: []Node{
						selector.Expr,
						Quoted(selector.Field),
						t.transpileExpr(stmt.Rhs[0]),
					},
				},
			}
		}

		// TODO could simplify this by implementing an IndexExpr node and then
		// performing a type switch on the transpiled result of lhs.
		if idx, ok := stmt.Lhs[0].(*ast.IndexExpr); ok {
			return &Statement{
				Expr: &BuiltInCall{
					Func: Literal("set"),
					Arguments: []Node{
						t.transpileExpr(idx.X),
						t.transpileExpr(idx.Index),
						t.transpileExpr(stmt.Rhs[0]),
					},
				},
			}
		}

		rhs := t.transpileExpr(stmt.Rhs[0])
		lhs := t.transpileExpr(stmt.Lhs[0])

		return &Assignment{RHS: rhs, LHS: lhs, New: stmt.Tok.String() == ":="}

	case *ast.RangeStmt:
		if _, isMap := t.typeOf(stmt.X).Underlying().(*types.Map); isMap {
			var body bytes.Buffer
			if err := format.Node(&body, t.Fset, stmt.Body); err != nil {
				panic(err)
			}

			// Super janky check to enforce deterministic iteration of maps
			// when side effects occur within the range's body.
			if bytes.Contains(body.Bytes(), []byte(`= append(`)) {
				return t.report(stmt, "ranges over maps are non-deterministic. use `helmette.SortedMap`")
			}
		}

		return &Range{
			Key:   t.transpileExpr(stmt.Key),
			Value: t.transpileExpr(stmt.Value),
			Over:  t.transpileExpr(stmt.X),
			Body:  t.transpileStatement(stmt.Body),
		}

	case *ast.ExprStmt:
		return &Statement{
			Expr: t.transpileExpr(stmt.X),
		}

	case *ast.BlockStmt:
		var out []Node
		for _, s := range stmt.List {
			out = append(out, t.transpileStatement(s))
		}
		return &Block{Statements: out}

	case *ast.IfStmt:
		return &IfStmt{
			Init: t.transpileStatement(stmt.Init),
			Cond: t.transpileExpr(stmt.Cond),
			Body: t.transpileStatement(stmt.Body),
			Else: t.transpileStatement(stmt.Else),
		}
	case *ast.ForStmt:
		var start, stop Node
		if b, ok := stmt.Cond.(*ast.BinaryExpr); ok {
			switch b.Op {
			case token.LSS, token.LEQ:
				if _, ok := b.X.(*ast.SelectorExpr); ok {
					start = t.transpileExpr(b.X)
				} else {
					switch declaration := b.X.(*ast.Ident).Obj.Decl.(type) {
					case *ast.AssignStmt:
						start = t.transpileExpr(declaration.Rhs[0])
					case *ast.Field:
						start = t.transpileExpr(declaration.Names[0])
					}
				}

				if _, ok := b.Y.(*ast.SelectorExpr); ok {
					stop = t.transpileExpr(b.Y)
				} else {
					switch declaration := b.Y.(*ast.Ident).Obj.Decl.(type) {
					case *ast.AssignStmt:
						stop = t.transpileExpr(declaration.Rhs[0])
					case *ast.Field:
						stop = t.transpileExpr(declaration.Names[0])
					}
				}
			case token.GTR, token.GEQ:
				if _, ok := b.X.(*ast.SelectorExpr); ok {
					stop = t.transpileExpr(b.X)
				} else {
					switch declaration := b.X.(*ast.Ident).Obj.Decl.(type) {
					case *ast.AssignStmt:
						stop = t.transpileExpr(declaration.Rhs[0])
					case *ast.Field:
						stop = t.transpileExpr(declaration.Names[0])
					}
				}

				if _, ok := b.Y.(*ast.SelectorExpr); ok {
					start = t.transpileExpr(b.Y)
				} else {
					switch declaration := b.Y.(*ast.Ident).Obj.Decl.(type) {
					case *ast.AssignStmt:
						start = t.transpileExpr(declaration.Rhs[0])
					case *ast.Field:
						start = t.transpileExpr(declaration.Names[0])
					}
				}
			default:
				return t.report(stmt, "%T of %s is not supported in for condition", b, b.Op)
			}
		}

		var step Node
		switch p := stmt.Post.(type) {
		case *ast.AssignStmt:
			if b, ok := p.Rhs[0].(*ast.BasicLit); ok && p.Tok == token.SUB_ASSIGN {
				b.Value = fmt.Sprintf("-%s", b.Value)
				// switch start with stop expression as step is decreasing
				step = start
				start = stop
				stop = step

				step = t.transpileExpr(b)
			} else {
				step = t.transpileExpr(b)
			}
		case *ast.IncDecStmt:
			switch p.Tok {
			case token.INC:
				step = Literal("1")
			case token.DEC:
				step = Literal("-1")
			}
		default:
			return t.report(stmt, "unhandled ast.ForStmt")
		}

		if stop == nil || start == nil || step == nil {
			return t.report(stmt, "start: %v; stop: %v; step: %v", start, stop, step)
		}
		return &Range{
			Key:   &Ident{Name: "_"},
			Value: Literal(fmt.Sprintf("$%s", stmt.Init.(*ast.AssignStmt).Lhs[0].(*ast.Ident).Name)),
			Over: &UntilStep{
				Start: start,
				Stop:  stop,
				Step:  step,
			},
			Body: t.transpileStatement(stmt.Body),
		}
	}

	return t.report(stmt, "unhandled ast.Stmt")
}

// transpileMVAssignStmt handles transpiling assignments where the RHS has
// exactly one expression and the LHS has more than one. E.g type checks, map
// checks, and functions with multiple return values.
func (t *Transpiler) transpileMVAssignStmt(stmt *ast.AssignStmt) Node {
	var stmts []Node

	// We need an intermediate variable to assign to. We'll synthesize it from
	// the identifiers on the LHS and position of the AssignStmt to get something that is unique,
	// deterministic, and mildly human readable.
	// foo, ok := ... -> $_123_foo_ok := ...
	// NB: Line number is used here as .Pos seems to be unstable.
	intermediate := &Ident{Name: fmt.Sprintf("_%d", t.Fset.Position(stmt.Pos()).Line)}
	for _, ident := range stmt.Lhs {
		intermediate.Name += "_"
		intermediate.Name += ident.(*ast.Ident).Name
	}

	// Next, check for special cases. The behavior of operations like x[key] or
	// x.(type) are dependent on the LHS of the assignment.
	var rhs Node
	if len(stmt.Lhs) == 2 && len(stmt.Rhs) == 1 {
		switch n := stmt.Rhs[0].(type) {
		case *ast.IndexExpr:
			rhs = litCall("_shims.dicttest", t.transpileExpr(n.X), t.transpileExpr(n.Index), t.zeroOf(t.typeOf(stmt.Lhs[0])))

		case *ast.TypeAssertExpr:
			typ := t.typeOf(n.Type)
			rhs = litCall("_shims.typetest", t.transpileTypeRepr(typ), t.transpileExpr(n.X), t.zeroOf(typ))
		}
	}

	// If we didn't hit any special cases, this is probably just a call with
	// multiple returns. Transpile as normal.
	if rhs == nil {
		rhs = t.transpileExpr(stmt.Rhs[0])
	}

	// Our intermediate variable will always be a new assignment.
	stmts = append(stmts, &Assignment{LHS: intermediate, New: true, RHS: rhs})

	// Generate our "unwrapping"
	// x, ok := ...
	// becomes:
	// _123_x_ok := ...
	// x := _123_x_ok[0]
	// ok := _123_x_ok[1]
	for i, ident := range stmt.Lhs {
		stmts = append(stmts, &Assignment{
			LHS: t.transpileExpr(ident),
			New: stmt.Tok.String() == ":=",
			RHS: t.maybeCast(&BuiltInCall{Func: Literal("index"), Arguments: []Node{
				intermediate,
				Literal(fmt.Sprintf("%d", i)),
			}}, t.typeOf(ident)),
		})
	}

	return &Block{Statements: stmts}
}

func (t *Transpiler) transpileExpr(n ast.Expr) Node {
	switch n := n.(type) {
	case nil:
		return nil

	case *ast.BasicLit:
		return t.maybeCast(Literal(n.Value), t.typeOf(n))

	case *ast.ParenExpr:
		return &ParenExpr{Expr: t.transpileExpr(n.X)}

	case *ast.StarExpr:
		// TODO this should be wrapped in something like "Assert not nil"
		return t.transpileExpr(n.X)

	case *ast.SliceExpr:
		target := t.transpileExpr(n.X)
		low := t.transpileExpr(n.Low)
		high := t.transpileExpr(n.High)
		max := t.transpileExpr(n.Max)

		// If low isn't specified it defaults to zero
		if low == nil {
			low = Literal("0")
		}

		// The builtin `slice` function from go would work great here but sprig
		// overwrites it for some reason with a worse version.
		if t.isString(n.X) {
			// NB: Triple slicing a string (""[1:2:3]) isn't valid. No need to
			// check .Max or .Slice3.

			// Empty slicing a string (""[:]) is effectively a noop
			if low == nil && high == nil {
				return target
			}

			// Sprig's substring will run [start:] if end is < 0.
			if high == nil {
				high = Literal("-1")
			}

			return &BuiltInCall{Func: Literal("substr"), Arguments: []Node{low, high, target}}
		}

		args := []Node{target, low}
		if high != nil {
			args = append(args, high)
		}
		if n.Slice3 && n.Max != nil {
			args = append(args, max)
		}
		return &BuiltInCall{Func: Literal("mustSlice"), Arguments: args}

	case *ast.CompositeLit:

		// TODO: Need to handle implementors of json.Marshaler.
		// TODO: Need to filter out zero value fields that are explicitly
		// provided.

		typ := types.Unalias(t.typeOf(n))
		if p, ok := typ.(*types.Pointer); ok {
			typ = p.Elem()
		}

		switch underlying := typ.Underlying().(type) {
		case *types.Slice:
			var elts []Node
			for _, el := range n.Elts {
				elts = append(elts, t.transpileExpr(el))
			}
			return &BuiltInCall{
				Func:      Literal("list"),
				Arguments: elts,
			}

		case *types.Map:
			assignable := types.AssignableTo(underlying.Key(), types.Typ[types.String])
			convertable := types.ConvertibleTo(underlying.Key(), types.Typ[types.String])
			if !(assignable || convertable) {
				panic(fmt.Sprintf("map keys must be assignable or convertable to strings. Got %s", underlying.Key()))
			}

			var d DictLiteral
			for _, el := range n.Elts {
				d.KeysValues = append(d.KeysValues, &KeyValue{
					Key:   t.transpileExpr(el.(*ast.KeyValueExpr).Key),
					Value: t.transpileExpr(el.(*ast.KeyValueExpr).Value),
				})
			}
			return &d

		case *types.Struct:
			zero := t.zeroOf(typ)
			fields := t.getFields(underlying)
			fieldByName := map[string]*structField{}
			for _, f := range fields {
				f := f
				fieldByName[f.Field.Name()] = &f
			}

			var embedded []Node
			var d DictLiteral
			for _, el := range n.Elts {
				key := el.(*ast.KeyValueExpr).Key.(*ast.Ident).Name
				value := el.(*ast.KeyValueExpr).Value

				field := fieldByName[key]
				if field.JSONOmit() {
					continue
				}

				if field.JSONInline() {
					embedded = append(embedded, t.transpileExpr(value))
					continue
				}

				d.KeysValues = append(d.KeysValues, &KeyValue{
					Key:   Quoted(field.JSONName()),
					Value: t.transpileExpr(value),
				})
			}

			args := []Node{zero}
			args = append(args, embedded...)
			args = append(args, &d)

			return &BuiltInCall{
				Func:      Literal("mustMergeOverwrite"),
				Arguments: args,
			}

		default:
			panic(fmt.Sprintf("unsupported composite literal %#v", typ))
		}

	case *ast.CallExpr:
		return t.transpileCallExpr(n)

	case *ast.Ident:
		switch obj := t.TypesInfo.ObjectOf(n).(type) {
		case *types.Const:
			return t.transpileConst(obj)

		case *types.Nil:
			return &Nil{}

		case *types.Var:
			return &Ident{Name: obj.Name()}

		case *types.Func:
			if _, ok := t.dependencies[obj.Pkg().Path()]; !ok {
				return t.report(n, "function %q is not present in the dependencies list and therefore cannot be referenced", obj.FullName())
			}

			// Function references are stored as []{funcName, capturedArguments...}.
			// See transpileCallExpr for details about this format.
			return &BuiltInCall{
				Func: Literal("list"),
				Arguments: []Node{
					Quoted(
						fmt.Sprintf("%s.%s", t.namespaceFor(obj.Pkg()), t.funcNameFor(obj)),
					),
				},
			}

		// Unclear how often this check is correct. true, false, and _ won't
		// have an Obj. AST rewriting can also result in .Obj being nil.
		case nil:
			if n.Name == "_" {
				return &Ident{Name: n.Name}
			}
			return Literal(n.Name)

		default:
			return t.report(n, "Unsupported *ast.Ident")
		}

	case *ast.SelectorExpr:
		switch obj := t.TypesInfo.ObjectOf(n.Sel).(type) {
		case *types.Const:
			return t.transpileConst(obj)

		case *types.Func:
			// Function references are stored as []{funcName, capturedArguments...}.
			// See transpileCallExpr for details about this format.

			args := []Node{
				Quoted(fmt.Sprintf("%s.%s", t.namespaceFor(obj.Pkg()), t.funcNameFor(obj))),
			}

			if obj.Signature().Recv() != nil {
				recv := t.transpileExpr(n.X)

				// If recv isn't a pointer, deepCopy it at the point of being
				// referenced to emulate its immutability.
				if _, ok := obj.Signature().Recv().Type().(*types.Pointer); !ok {
					recv = &BuiltInCall{Func: Literal("deepCopy"), Arguments: []Node{recv}}
				}

				args = append(args, recv)
			}

			return &BuiltInCall{
				Func:      Literal("list"),
				Arguments: args,
			}

		case *types.Var:
			// If our selector is a variable, we're probably accessing a field
			// on a struct.
			typ := types.Unalias(t.typeOf(n.X))
			if p, ok := typ.(*types.Pointer); ok {
				typ = p.Elem()
			}

			// pod.Metadata.Name = "foo" -> (pod.name)
			for _, f := range t.getFields(typ.Underlying().(*types.Struct)) {
				if f.Field.Name() == n.Sel.Name {
					return t.maybeCast(&Selector{
						Expr:    t.transpileExpr(n.X),
						Field:   f.JSONName(),
						Inlined: f.JSONInline(),
					}, t.typeOf(n))
				}
			}
		}

		return t.report(n, "%T", t.TypesInfo.ObjectOf(n.Sel))

	case *ast.BinaryExpr:
		// Closure helpers to make the following logic a bit nicer.

		builtin := func(name string, args ...Node) Node {
			return &BuiltInCall{Func: Literal(name), Arguments: args}
		}

		f := func(op string) func(a, b Node) Node {
			return func(a, b Node) Node {
				return builtin(op, a, b)
			}
		}

		wrapWithCast := func(op, cast string) func(a, b Node) Node {
			return func(a, b Node) Node {
				return &Cast{To: cast, X: &BuiltInCall{Func: Literal(op), Arguments: []Node{a, b}}}
			}
		}

		// Nasty workaround to support versions of helm compiled with go < 1.19.
		// text/template failed to handle $var ==/!= nil. To get this to work
		// we marshal $var to json and compare to the string literal "null".
		//
		// See also:
		// - https://github.com/redpanda-data/helm-charts/issues/1454
		// - https://github.com/golang/go/commit/c58f1bb65f2187d79a5842bb19f4db4cafd22794#diff-eaf3618e0348f6d918eede2b03dd275ed9129dcdf10d7cf470137c1af7c755f4L474
		// - TestTemplateHelm310 in charts/redpanda
		go119eq := func(op string, side string) func(a, b Node) Node {
			return func(a, b Node) Node {
				switch side {
				case "lhs":
					return builtin(op, builtin("toJson", a), Quoted("null"))
				case "rhs":
					return builtin(op, builtin("toJson", b), Quoted("null"))
				default:
					panic("side should only be lhs or rhs")
				}
			}
		}

		strConcat := func(a, b Node) Node {
			return builtin("printf", Quoted("%s%s"), a, b)
		}

		// Poor man's pattern matching :[
		mapping := map[[3]string]func(a, b Node) Node{
			{"_", token.EQL.String(), "_"}:  f("eq"),
			{"_", token.NEQ.String(), "_"}:  f("ne"),
			{"_", token.LAND.String(), "_"}: f("and"),
			{"_", token.LOR.String(), "_"}:  f("or"),
			{"_", token.GTR.String(), "_"}:  f("gt"),
			{"_", token.LSS.String(), "_"}:  f("lt"),
			{"_", token.GEQ.String(), "_"}:  f("ge"),
			{"_", token.LEQ.String(), "_"}:  f("le"),

			{"_", token.EQL.String(), "untyped nil"}: go119eq("eq", "lhs"),
			{"_", token.NEQ.String(), "untyped nil"}: go119eq("ne", "lhs"),
			{"untyped nil", token.EQL.String(), "_"}: go119eq("eq", "rhs"),
			{"untyped nil", token.NEQ.String(), "_"}: go119eq("ne", "rhs"),

			// Support for string + string!
			{"string", token.ADD.String(), "string"}:                 strConcat,
			{"string", token.ADD.String(), "untyped string"}:         strConcat,
			{"untyped string", token.ADD.String(), "string"}:         strConcat,
			{"untyped string", token.ADD.String(), "untyped string"}: strConcat,

			{"float32", token.ADD.String(), "float32"}: wrapWithCast("addf", "float64"),
			{"float32", token.MUL.String(), "float32"}: wrapWithCast("mulf", "float64"),
			{"float32", token.QUO.String(), "float32"}: wrapWithCast("divf", "float32"),
			{"float32", token.SUB.String(), "float32"}: wrapWithCast("subf", "float64"),

			{"float64", token.ADD.String(), "float64"}: wrapWithCast("addf", "float64"),
			{"float64", token.MUL.String(), "float64"}: wrapWithCast("mulf", "float64"),
			{"float64", token.QUO.String(), "float64"}: wrapWithCast("divf", "float64"),
			{"float64", token.SUB.String(), "float64"}: wrapWithCast("subf", "float64"),

			{"int", token.ADD.String(), "int"}: wrapWithCast("add", "int"),
			{"int", token.MUL.String(), "int"}: wrapWithCast("mul", "int"),
			{"int", token.QUO.String(), "int"}: wrapWithCast("div", "int"),
			{"int", token.REM.String(), "int"}: wrapWithCast("mod", "int"),
			{"int", token.SUB.String(), "int"}: wrapWithCast("sub", "int"),

			{"int32", token.ADD.String(), "int32"}: wrapWithCast("add", "int"),
			{"int32", token.MUL.String(), "int32"}: wrapWithCast("mul", "int"),
			{"int32", token.QUO.String(), "int32"}: wrapWithCast("div", "int"),
			{"int32", token.REM.String(), "int32"}: wrapWithCast("mod", "int"),
			{"int32", token.SUB.String(), "int32"}: wrapWithCast("sub", "int"),

			{"int64", token.ADD.String(), "int64"}: wrapWithCast("add", "int64"),
			{"int64", token.MUL.String(), "int64"}: wrapWithCast("mul", "int64"),
			{"int64", token.QUO.String(), "int64"}: wrapWithCast("div", "int64"),
			{"int64", token.REM.String(), "int64"}: wrapWithCast("mod", "int64"),
			{"int64", token.SUB.String(), "int64"}: wrapWithCast("sub", "int64"),

			{"untyped int", token.ADD.String(), "untyped int"}: f("add"),
			{"untyped int", token.MUL.String(), "untyped int"}: f("mul"),
			{"untyped int", token.QUO.String(), "untyped int"}: f("div"),
			{"untyped int", token.REM.String(), "untyped int"}: f("mod"),
			{"untyped int", token.SUB.String(), "untyped int"}: f("sub"),

			{"untyped float", token.ADD.String(), "untyped float"}: f("addf"),
			{"untyped float", token.MUL.String(), "untyped float"}: f("mulf"),
			{"untyped float", token.QUO.String(), "untyped float"}: f("divf"),
			{"untyped float", token.SUB.String(), "untyped float"}: f("subf"),
		}

		// Iterate though "patterns" in order of priority.
		patterns := [][3]string{
			{t.typeOf(n.X).String(), n.Op.String(), t.typeOf(n.Y).String()},
			{"_", n.Op.String(), t.typeOf(n.Y).String()},
			{t.typeOf(n.X).String(), n.Op.String(), "_"},
			{"_", n.Op.String(), "_"},
		}

		for _, pattern := range patterns {
			if funcName, ok := mapping[pattern]; ok {
				return funcName(t.transpileExpr(n.X), t.transpileExpr(n.Y))
			}
		}

		return t.report(n, `No matching %T signature for %v`, n, patterns)

	case *ast.UnaryExpr:
		switch n.Op {
		case token.NOT:
			return &BuiltInCall{
				Func:      Literal("not"),
				Arguments: []Node{t.transpileExpr(n.X)},
			}
		case token.AND:
			// Can't take addresses in templates so just return the variable.
			return t.transpileExpr(n.X)
		case token.SUB:
			if i, ok := n.X.(*ast.BasicLit); ok {
				return Literal(fmt.Sprintf("-%s", i.Value))
			}
		}

	case *ast.IndexExpr:
		// IndexExprs handle (n.X)[n.Index] as an expression i.e. map tests (_,
		// _ = X[Index]) are NOT handled here (see
		// [Transpiler.transpileMVAssignStmt]).

		switch typ := t.typeOf(n.X).Underlying().(type) {
		case *types.Map:
			// if the zero value of elem's type is NOT nil, we need to
			// replicate go's behavior of returning zero values on missing
			// keys.
			elemZero := t.zeroOf(typ.Elem())
			if _, ok := elemZero.(*Nil); !ok {
				// This behavior is replicated using sprig's ternary function.
				// Admittedly, this could cause unexpected behavior if either
				// expr or index are particularly complex or have side effects.
				// In practice, this, thankfully, has not been the case.
				expr := t.transpileExpr(n.X)
				index := t.transpileExpr(n.Index)

				// X[index] -> (ternary (index X index) (zero) (hasKey X index))
				return &BuiltInCall{
					// Ternary signature is (trueVal, falseVal, testVal)
					Func: Literal("ternary"),
					Arguments: []Node{
						&BuiltInCall{
							Func:      Literal("index"),
							Arguments: []Node{expr, index},
						},
						elemZero,
						&BuiltInCall{
							Func:      Literal("hasKey"),
							Arguments: []Node{expr, index},
						},
					},
				}
			}
		}

		// Otherwise, text/template's builtin index function handles everything
		// for us as it returns nil for missing keys due to all maps being
		// `map[string]any`'s.
		return &BuiltInCall{
			Func: Literal("index"),
			Arguments: []Node{
				t.transpileExpr(n.X),
				t.transpileExpr(n.Index),
			},
		}

	case *ast.TypeAssertExpr:
		typ := t.typeOf(n.Type)

		if basic, ok := typ.(*types.Basic); ok && (basic.Info()&types.IsNumeric != 0) {
			return t.report(n, "type assertions on numeric types are unreliable due to JSON casting all numbers to float64's. Instead use `helmette.IsNumeric` or `helmette.AsIntegral`")
		}

		return litCall(
			"_shims.typeassertion",
			t.transpileTypeRepr(typ),
			t.transpileExpr(n.X),
		)
	}

	var b bytes.Buffer
	if err := format.Node(&b, t.Fset, n); err != nil {
		panic(err)
	}
	panic(fmt.Sprintf("unhandled Expr %T\n%s", n, b.String()))
}

func (t *Transpiler) transpileCallExpr(n *ast.CallExpr) Node {
	callee := typeutil.Callee(t.TypesInfo, n)

	// n.Fun is not a function, signature, var, or built in.
	// Chances are it's a cast expression.
	if callee == nil {
		return t.transpileCast(n.Args[0], t.typeOf(n.Fun))
	}

	var args []Node
	for _, arg := range n.Args {
		args = append(args, t.transpileExpr(arg))
	}

	// go builtins
	if builtin, ok := callee.(*types.Builtin); ok {
		switch builtin.Name() {
		case "append":
			// Sprig's append is a bit frustrating to work with as it doesn't
			// handle `nil` slices nor multiple elements.
			// Instead we turn appends into concat calls.

			// To handle `nil` slices, wrap them in a call to default with an
			// empty slice.
			// append(x) -> (append (default (list) x))
			defaultToEmpty := func(n Node) Node {
				return &BuiltInCall{
					Func:      Literal("default"),
					Arguments: []Node{&BuiltInCall{Func: Literal("list")}, n},
				}
			}

			// If a spread is specified, concat the two slices.
			// NB: A spread with individual elements is NOT valid go syntax
			// (e.g. append(x, 1, 2, y...)).
			//
			// append(x, y...) -> (concat x y)
			if n.Ellipsis.IsValid() {
				return &BuiltInCall{
					Func: Literal("concat"),
					Arguments: []Node{
						defaultToEmpty(args[0]),
						defaultToEmpty(args[len(args)-1]),
					},
				}
			}

			// If no spread, just concat the arguments list.
			return &BuiltInCall{
				Func: Literal("concat"),
				Arguments: []Node{
					defaultToEmpty(args[0]),
					&BuiltInCall{Func: Literal("list"), Arguments: args[1:]},
				},
			}
		case "panic":
			return &BuiltInCall{Func: Literal("fail"), Arguments: args}
		case "len":
			return t.maybeCast(litCall("_shims.len", args...), types.Typ[types.Int])
		case "delete":
			return &BuiltInCall{Func: Literal("unset"), Arguments: args}
		default:
			panic(fmt.Sprintf("unsupport golang builtin %q", n.Fun.(*ast.Ident).Name))
		}
	}

	id := callee.Pkg().Path() + "." + callee.Name()

	signature := t.typeOf(n.Fun).(*types.Signature)

	var receiver Node
	if recv := callee.Type().(*types.Signature).Recv(); recv != nil {
		// If a function invocation has a receiver, it's reasonably safe to
		// assume that it's a SelectorExpr as the form should always be
		// `mystruct.MyMethod`.
		// In theory this could panic in the case of something like:
		// `x := mystruct.MyMethod; x()`
		// That's not supported any how.
		receiver = t.transpileExpr(n.Fun.(*ast.SelectorExpr).X)

		receiverName := ""
		switch x := types.Unalias(recv.Type()).(type) {
		case *types.Named:
			receiverName = x.Obj().Name()
		case *types.Pointer:
			receiverName = "*" + types.Unalias(x.Elem()).(*types.Named).Obj().Name()
		}
		// Try to make a string that looks like something from reflect.
		id = callee.Pkg().Path() + ".(" + receiverName + ")." + callee.Name()
	}

	// Before checking anything else, search for a +gotohelm:builtin=X
	// directive. If we find such a directive, we'll emit a BuiltInCall node
	// with the contents of the directive. The results are cached in t.builtins
	// as an optimization.
	if _, ok := t.builtins[id]; !ok {
		t.builtins[id] = ""
		t.builtins[id] = t.directivesOf(callee)["builtin"]
	}

	// The above would have populated the builtins cache for us. If we have a
	// hit, construct a BuiltInCall.
	if builtin := t.builtins[id]; builtin != "" {
		if signature.Results().Len() < 2 {
			return &BuiltInCall{Func: Literal(builtin), Arguments: args}
		}

		// Special case, if the return signature is (T, error). We'll
		// automagically wrap the builtin invocation with (list CALLEXPR nil)
		// so it looks like this function returns an error similar to its go
		// counter part. In reality, there's no error handling in templates as
		// the template execution will be halted whenever a helper returns a
		// non-nil error.
		if named, ok := signature.Results().At(1).Type().(*types.Named); ok && named.Obj().Pkg() == nil && named.Obj().Name() == "error" {
			return &BuiltInCall{
				Func: Literal("list"),
				Arguments: []Node{
					&BuiltInCall{Func: Literal(builtin), Arguments: args},
					Literal("nil"),
				},
			}
		}

		return t.report(n, "unsupported usage of builtin directive for signature: %v", signature)
	}

	// The second to last stop on this train, the big ol' switch statement of
	// special cases. We match on the fully qualified function name and
	// manually handle the transpilation on a case by case basis.
	// This might include:
	// - convent functions that we polyfill (ptr.To)
	// - stdlib functions that just need to have some minor tweaks to map to
	//   sprig funcs (strings.TrimSuffix)
	// - helmette utilities

	// TEMPORARY HACK: helmette has moved from into it's own module under
	// gotohelm but we need to support transpiling the old import paths for a
	// little while.
	if strings.HasPrefix(id, "github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette") {
		id = strings.Replace(id, "github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette", "github.com/redpanda-data/redpanda-operator/gotohelm/helmette", 1)
	}

	switch id {
	case "sort.Strings":
		return &BuiltInCall{Func: Literal("sortAlpha"), Arguments: args}
	case "slices.Sorted":
		// sprig has no generic sort. sortAlpha coerces everything it's handed
		// to a string, so reject anything that isn't already a string rather
		// than silently mangling values.
		elem := signature.Results().At(0).Type().(*types.Slice).Elem()
		if basic, ok := elem.Underlying().(*types.Basic); !ok || basic.Info()&types.IsString == 0 {
			return t.report(n, "slices.Sorted is only supported for strings, got: %v", elem)
		}
		return litCall("_shims.slices_Sorted", args...)
	case "cmp.Or":
		// Provided the return is a Basic type, cmp.Or can be transpiled to default.
		// Other types result in divergent behaviors:
		// - Structs - Don't really exist in helm but if they do, sprig considers
		//   them always not-empty
		// - Pointers - Don't exist in helm so the test is on the pointed to value
		//   not the pointer itself.
		//   cmp.Or(new(""), new("fallback")) -> "" in go.
		//   default "" "fallback" -> "fallback" in helm.
		//
		// Reject anything that isn't a basic type rather than diverging.
		if _, ok := signature.Results().At(0).Type().Underlying().(*types.Basic); !ok {
			panic(&Unsupported{
				Fset: t.Fset,
				Node: n,
				Msg:  fmt.Sprintf("cmp.Or is only supported for basic types, got: %v", signature.Results().At(0).Type()),
			})
		}

		// Fold right to left so cmp.Or(a, b, c) becomes
		// `(default (default c b) a)`.
		folded := args[len(args)-1]
		for i := len(args) - 2; i >= 0; i-- {
			folded = &BuiltInCall{Func: Literal("default"), Arguments: []Node{folded, args[i]}}
		}
		return folded
	case "strings.Join":
		return &BuiltInCall{Func: Literal("join"), Arguments: []Node{args[1], args[0]}}
	case "strings.Contains":
		return &BuiltInCall{Func: Literal("contains"), Arguments: []Node{args[1], args[0]}}
	case "strings.TrimSuffix":
		return &BuiltInCall{Func: Literal("trimSuffix"), Arguments: []Node{args[1], args[0]}}
	case "strings.TrimPrefix":
		return &BuiltInCall{Func: Literal("trimPrefix"), Arguments: []Node{args[1], args[0]}}
	case "strings.HasPrefix":
		return &BuiltInCall{Func: Literal("hasPrefix"), Arguments: []Node{args[1], args[0]}}
	case "strings.ReplaceAll":
		return &BuiltInCall{Func: Literal("replace"), Arguments: []Node{args[1], args[2], args[0]}}
	case "k8s.io/apimachinery/pkg/util/intstr.FromInt32", "k8s.io/apimachinery/pkg/util/intstr.FromInt", "k8s.io/apimachinery/pkg/util/intstr.FromString":
		return args[0]
	case "k8s.io/utils/ptr.Deref":
		return t.maybeCast(litCall("_shims.ptr_Deref", args...), signature.Results().At(0).Type())
	case "k8s.io/utils/ptr.To":
		return args[0]
	case "k8s.io/utils/ptr.Equal":
		return litCall("_shims.ptr_Equal", args...)
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Dig":
		return &BuiltInCall{Func: Literal("dig"), Arguments: append(args[2:], args[1], args[0])}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Unwrap":
		return &Selector{Expr: args[0], Field: "AsMap"}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.UnmarshalInto":
		return args[0]
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.AsIntegral":
		return litCall("_shims.asintegral", args...)
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.AsNumeric":
		return litCall("_shims.asnumeric", args...)
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Merge":
		dict := DictLiteral{}
		return &BuiltInCall{Func: Literal("merge"), Arguments: append([]Node{&dict}, args...)}
	// Add note about this change.
	case "helm.sh/helm/v3/pkg/chartutil.(Values).AsMap":
		return &Selector{Expr: receiver, Field: "AsMap"}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.MergeTo":
		dict := DictLiteral{}
		return &BuiltInCall{Func: Literal("merge"), Arguments: append([]Node{&dict}, args...)}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.SortedKeys":
		return &BuiltInCall{Func: Literal("sortAlpha"), Arguments: []Node{&BuiltInCall{Func: Literal("keys"), Arguments: args}}}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Get":
		return litCall("_shims.get", args...)
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.FromYaml":
		return litCall("_shims.fromYaml", args...)
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.SortedMap":
		// In go, map iteration is non-deterministic which can cause
		// differences in go vs helm output. A ruleguard linter is responsible
		// for enforcing the usage of this helper. text/template's range node
		// will automagically sort maps keys, where applicable. So this helper
		// "transpiles away".
		return args[0]

	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Tpl":
		// In go land, tpl requires a handle to the chart's .Dot so we can get
		// the chart's templates. Helm doesn't require this as that's just how
		// tpl works. So just clip out the fist argument.
		return &BuiltInCall{Func: Literal("tpl"), Arguments: args[1:]}

	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.Lookup":
		// Super ugly but it's fairly safe to assume that the return type of
		// Lookup will always be a pointer as only pointers implement
		// kube.Object.
		// Type params are difficult to work with so its easiest to extract the
		// return value (Which it a generic in Lookup) of the "instance" of the
		// function signature.
		returns := types.Unalias(signature.Results().At(0).Type()).(*types.Pointer)
		k8sType := types.Unalias(returns.Elem()).(*types.Named).Obj()

		// Step through the client set's Scheme to automatically infer the
		// APIVersion and Kind of objects. We don't want any accidental typos
		// or mistyping to occur.
		for gvk, typ := range scheme.Scheme.AllKnownTypes() {
			if typ.PkgPath() == k8sType.Pkg().Path() && typ.Name() == k8sType.Name() {
				apiVersion, kind := gvk.ToAPIVersionAndKind()

				// Inject the apiVersion and kind as arguments and snip `dot`
				// from the arguments list.
				args := append([]Node{Quoted(apiVersion), Quoted(kind)}, args[1:]...)

				return litCall("_shims.lookup", args...)
			}
		}

		// If we couldn't find the object in the scheme, panic. It's probably
		// due to the usage of a 3rd party resource. If you hit this, just
		// inject a Scheme into the transpiler instead of relying on the kube
		// client's builtin scheme.
		panic(fmt.Sprintf("unrecognized type: %v", k8sType))

	case "time.ParseDuration":
		return litCall("_shims.time_ParseDuration", args...)

	case "time.(Duration).String":
		return litCall("_shims.time_Duration_String", args...)

	// .Files support
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.(*Files).Get":
		return &BuiltInCall{Func: &Selector{Expr: receiver, Field: "Get"}, Arguments: args}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.(*Files).GetBytes":
		return &BuiltInCall{Func: &Selector{Expr: receiver, Field: "GetBytes"}, Arguments: args}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.(*Files).Lines":
		return &BuiltInCall{Func: &Selector{Expr: receiver, Field: "Lines"}, Arguments: args}
	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.(*Files).Glob":
		return &BuiltInCall{Func: &Selector{Expr: receiver, Field: "Glob"}, Arguments: args}

	case "github.com/redpanda-data/redpanda-operator/gotohelm/helmette.MustDuration":
		return litCall(
			"_shims.time_Duration_String",
			litCall("_shims.time_ParseDuration", args...),
		)

	// Support for resource.Quantity. In helm world, resource.Quantity is
	// always represented as it's JSON representation of either a string or a
	// number with polyfills in the bootstrap package for the following
	// methods.
	// WARNING: There is not 100% compatibility and this is on purpose.
	case "k8s.io/apimachinery/pkg/api/resource.MustParse":
		return litCall("_shims.resource_MustParse", args...)
	case "k8s.io/apimachinery/pkg/api/resource.(*Quantity).Value":
		return &Cast{To: "int64", X: litCall("_shims.resource_Value", append([]Node{receiver}, args...)...)}
	case "k8s.io/apimachinery/pkg/api/resource.(*Quantity).MilliValue":
		return &Cast{To: "int64", X: litCall("_shims.resource_MilliValue", append([]Node{receiver}, args...)...)}
	case "k8s.io/apimachinery/pkg/api/resource.(*Quantity).String":
		// Similarly to DeepCopy, we're exploit the JSON representation of
		// resource.Quantity here and rely on MustParse to simply normalize the
		// representation.
		return litCall("_shims.resource_MustParse", receiver)
	case "k8s.io/apimachinery/pkg/api/resource.(Quantity).DeepCopy":
		// DeepCopy is supported in a bit of a hacky way. We call "MustParse"
		// which takes the JSON (string) representation of a resource.Quantity
		// and parses it returning a new resource.Quantity, which is
		// functionally equivalent. It has the added benefit of normalizing the
		// string form of the Quantity.
		return litCall("_shims.resource_MustParse", receiver)
	}

	// Final stop, all our special cases have been handled. Either this call is
	// going to a transpiled function or it's not supported. For this, we
	// consult .dependencies which will have any dependencies (subcharts) and
	// the package that's currently being transpiled.
	if _, ok := t.dependencies[callee.Pkg().Path()]; !ok {
		panic(fmt.Sprintf("unsupported function %q", id))
	}

	// We've got a function call to something in our dependencies. All we're
	// going to do is transpile the call itself. The function itself will get
	// transpiled when the transpiler gets there or it'll be provided via
	// helm's subcharting functionality.
	var call Node

	// Method call.
	if r := callee.Type().(*types.Signature).Recv(); r == nil {
		switch callee := callee.(type) {
		case *types.Func:
			// Easy case: if there's no receiver, this is just a function call.
			call = litCall(fmt.Sprintf("%s.%s", t.namespaceFor(callee.Pkg()), t.funcNameFor(callee)), args...)
		case *types.Var:
			// If callee is a var, we're calling a function variable. e.g.:
			// func myfn() {}
			// x := myfn
			// x()
			// OR
			// type doer struct{}
			// x := doer{}
			// x.Do()
			//
			// To support both (and leave a bit of room for closures in the
			// future), functions are captured as a slice where [0] is the name
			// of the function's template and [1:] is any additional arguments,
			// namely a reference to the receiver.
			//
			// To invoke such functions, we need to make use of the
			// "Encapsulator" member on Call which allows us to control the
			// mapping of []Node arguments to a single Node for gotohelm's
			// calling convention. This let's us use concat and rest which
			// avoids the need to perform additional checks to distinguish
			// between a function and method.
			call = &Call{
				FuncName: &BuiltInCall{
					Func: Literal("first"),
					Arguments: []Node{
						t.transpileExpr(n.Fun),
					},
				},
				Encapsulator: Literal("concat"),
				Arguments: []Node{
					&BuiltInCall{
						Func: Literal("rest"),
						Arguments: []Node{
							t.transpileExpr(n.Fun),
						},
					},
					&BuiltInCall{
						Func:      Literal("list"),
						Arguments: args,
					},
				},
			}
		default:
			return t.report(n, "callee of type %T: %v", callee, callee)
		}
	} else {
		// Otherwise, if there is a receiver, we need to emulate a method call.
		typ := types.Unalias(r.Type())

		mutable := false
		switch t := typ.(type) {
		case *types.Pointer:
			typ = types.Unalias(t.Elem())
			mutable = true
		}

		if _, ok := typ.(*types.Named); !ok {
			return t.report(n, "method calls with not pointer type with named type")
		}
		var receiverArg Node

		// When receiver is a pointer then dictionary can be passed as is.
		// When receiver is not a pointer then dictionary is deep copied to emulate immutability.
		receiverArg = &BuiltInCall{Func: Literal("deepCopy"), Arguments: []Node{t.transpileExpr(n.Fun.(*ast.SelectorExpr).X)}}
		if mutable {
			receiverArg = t.transpileExpr(n.Fun.(*ast.SelectorExpr).X)
		}

		call = litCall(
			fmt.Sprintf("%s.%s", t.namespaceFor(callee.Pkg()), t.funcNameFor(callee.(*types.Func))),
			// Method calls come in as a "top level" CallExpr where .Fun is the
			// selector up to that call. e.g. `Foo.Bar.Baz()` will be a `CallExpr`.
			// It's `.Fun` is a `SelectorExpr` where `.X` is `Foo.Bar`, the receiver,
			// and `.Sel` is `Baz`, the method name.
			append([]Node{receiverArg}, args...)...,
		)
	}

	// If there's only a single return value, we'll possibly want to wrap
	// the value in a cast for safety. If there are multiple return values,
	// any casting will be handled by the transpilation of selector
	// expressions.
	if signature.Results().Len() == 1 {
		return t.maybeCast(call, signature.Results().At(0).Type())
	}

	return call
}

func (t *Transpiler) transpileCast(expr ast.Expr, to types.Type) Node {
	from := t.typeOf(expr)
	node := t.transpileExpr(expr)

	// NB: We use .Underlying() here to unwrap any type wrappers or alias as
	// there's no such concept in helm world:
	//
	// type myint int
	// myint(10)
	switch typ := to.Underlying().(type) {
	case *types.Basic:
		switch typ.Kind() {
		case types.Int, types.Int32:
			return &Cast{X: node, To: "int"}
		case types.Int64:
			return &Cast{X: node, To: "int64"}
		case types.Float64:
			return &Cast{X: node, To: "float64"}
		case types.String:
			if from.String() == "byte" {
				return &BuiltInCall{
					Func:      Literal("printf"),
					Arguments: append([]Node{Literal("\"%c\"")}, node),
				}
			}
			return &BuiltInCall{Func: Literal("toString"), Arguments: []Node{node}}
		}
	case *types.Interface:
		// Cast to any or interface{}.
		if typ.Empty() {
			return node
		}
	case *types.Struct:
		// If we're casting from an alias to the aliased type, do
		// nothing. e.g.:
		// type MySpecialType SomeOtherStruct
		// var x MySpecialType
		// SomeOtherStruct(x)
		if types.ConvertibleTo(from, to) {
			return node
		}
	}

	return t.report(expr, "unsupported type cast to %v", to)
}

func (t *Transpiler) transpileTypeRepr(typ types.Type) Node {
	// NB: Ideally, we'd just use typ.String(). Sadly, we can't as typ.String()
	// will return `any` but we need to match the result of fmt.Sprintf("%T")
	// which returns `interface {}`.
	switch typ := typ.(type) {
	case *types.Pointer:
		return &BuiltInCall{Func: Literal("printf"), Arguments: []Node{
			Quoted("*%s"),
			t.transpileTypeRepr(typ.Elem()),
		}}
	case *types.Array:
		return &BuiltInCall{Func: Literal("printf"), Arguments: []Node{
			Quoted(fmt.Sprintf("[%d]%%s", typ.Len())),
			t.transpileTypeRepr(typ.Elem()),
		}}
	case *types.Slice:
		return &BuiltInCall{Func: Literal("printf"), Arguments: []Node{
			Quoted("[]%s"),
			t.transpileTypeRepr(typ.Elem()),
		}}
	case *types.Map:
		return &BuiltInCall{Func: Literal("printf"), Arguments: []Node{
			Quoted("map[%s]%s"),
			t.transpileTypeRepr(typ.Key()),
			t.transpileTypeRepr(typ.Elem()),
		}}
	case *types.Basic:
		return Quoted(typ.String())
	case *types.Alias:
		return t.transpileTypeRepr(typ.Rhs())
	case *types.Interface:
		if typ.Empty() {
			return Quoted("interface {}")
		}
	}
	panic(fmt.Sprintf("unsupported type: %v", typ))
}

func (t *Transpiler) transpileConst(c *types.Const) Node {
	// We could include definitions to constants and then reference
	// them. For now, it's easier to turn constants into their
	// definitions.

	if c.Val().Kind() != constant.Float {
		return t.maybeCast(Literal(c.Val().ExactString()), c.Type())
	}

	// Floats are a bit weird. Go may store them as a quotient in some cases to
	// have an exact version of the value. .ExactString() will return the
	// quotient form (e.g. 0.1 is "1/10"). .String() will return a possibly
	// truncated value. The only other option is to get the value as a float64.
	// The second return value here is reporting if this is an exact
	// representation. It will be false for values like e, pi, and 0.1. That is
	// to say, there's not a reasonable way to handle it returning false. Given
	// that go's float64 values have the exact same problem, we're going to
	// ignore it for now and hope it's not a terrible mistake.
	as64, _ := constant.Float64Val(c.Val())
	return t.maybeCast(Literal(strconv.FormatFloat(as64, 'f', -1, 64)), c.Type())
}

func (t *Transpiler) isString(e ast.Expr) bool {
	return types.AssignableTo(t.TypesInfo.TypeOf(e), types.Typ[types.String])
}

func (t *Transpiler) typeOf(expr ast.Expr) types.Type {
	return t.TypesInfo.TypeOf(expr)
}

func (t *Transpiler) zeroOf(typ types.Type) Node {
	// NB: The special cases below are keyed off the type's string, which for an
	// alias is the alias' own name. Unalias so `type MyTime = metav1.Time`
	// still gets metav1.Time's treatment instead of falling through to the
	// json.Marshaler check below and panicking.
	typ = types.Unalias(typ)

	// Special cases.
	switch typ.String() {
	case "k8s.io/apimachinery/pkg/apis/meta/v1.Time":
		return &Nil{}
	case "k8s.io/apimachinery/pkg/util/intstr.IntOrString":
		// IntOrString's zero value appears to marshal to a 0 though it's
		// unclear how correct this is.
		return Literal("0")
	case "*k8s.io/apimachinery/pkg/api/resource.Quantity":
		return &Nil{}
	case "k8s.io/apimachinery/pkg/api/resource.Quantity":
		return Literal(`"0"`)
	}

	// If encoding/json is in the dependency chain for this package, we'll
	// enable some additional checks (because it's other wise very difficult to
	// check for implementation of {M,Unm}arshaller...)
	if json := importedPackage(t.Types, "encoding/json"); json != nil {
		marshaller := json.Scope().Lookup("Marshaler").Type().Underlying().(*types.Interface)
		unmarshaller := json.Scope().Lookup("Unmarshaler").Type().Underlying().(*types.Interface)

		ptr := types.NewPointer(typ)

		// If we can get a handle to Marshaler and Unmarshaler, we'll error out
		// on any types that implement either and aren't special cased as their
		// JSON representation likely won't match what we can infer from the go
		// type.
		switch {
		case types.Implements(typ, unmarshaller),
			types.Implements(typ, marshaller),
			types.Implements(ptr, marshaller),
			types.Implements(ptr, unmarshaller):
			panic(fmt.Sprintf("unsupported type %q implemented json.Unmarshaler or json.Marshaller and needs to be special cased but isn't currently", typ.String()))
		}
	}

	switch underlying := typ.Underlying().(type) {
	case *types.Basic:
		switch underlying.Info() {
		case types.IsString, types.IsUntyped | types.IsString:
			return Literal(`""`)
		case types.IsInteger, types.IsUnsigned | types.IsInteger:
			return Literal("0")
		case types.IsFloat:
			return &BuiltInCall{Func: Literal("float64"), Arguments: []Node{Literal("0")}}
		case types.IsBoolean:
			return Literal("false")
		default:
			panic(fmt.Sprintf("unsupported Basic type: %#v", typ))
		}

	case *types.Pointer, *types.Map, *types.Interface, *types.Slice, *types.Signature:
		return &Nil{}

	case *types.Struct:
		var out DictLiteral

		// Skip fields that json Marshalling would itself skip.
		for _, field := range t.getFields(underlying) {
			if field.JSONOmit() || !field.IncludeInZero() {
				continue
			}

			if field.JSONInline() {
				continue
			}

			out.KeysValues = append(out.KeysValues, &KeyValue{
				Key:   Quoted(field.JSONName()),
				Value: t.zeroOf(field.Field.Type()),
			})
		}
		return &out

	default:
		panic(fmt.Sprintf("unsupported type: %#v", typ))
	}
}

// getFields returns a _flattened_ list (embedded structs) of structFields for
// the given struct type.
func (t *Transpiler) getFields(root *types.Struct) []structField {
	var fields []structField

	for queue := []*types.Struct{root}; len(queue) > 0; queue = queue[1:] {
		s := queue[0]

		for i := range s.NumFields() {
			field := structField{
				Field: s.Field(i),
				Tag:   parseTag(s.Tag(i)),
			}

			// If we encounter a JSON inlined field (See JSONInline for
			// details), merge the embedded struct into our list of fields to
			// support direct access thereof, just list go.
			if field.JSONInline() {
				// NB: .Underlying() rather than asserting to *types.Named
				// first: an embedded field may be an alias (type A =
				// otherpkg.B), which is a *types.Alias, not a *types.Named.
				// Both resolve to the same struct through .Underlying().
				queue = append(queue, field.Field.Type().Underlying().(*types.Struct))
			}

			fields = append(fields, field)
		}
	}

	return fields
}

// maybeCast may wrap the provided [Node] with a [Cast] to the provided
// [types.Type] if it's possible that text/template or sprig would misinterpret
// the value.
// For example: go can infer that `1` should be a float64 in some situations.
// text/template would require an explicit cast.
func (t *Transpiler) maybeCast(n Node, to types.Type) Node {
	// In some cases, to may be nil due to the return of `t.typeOf` e.g. a
	// blank identifier. There's no need to cast in such cases, so return n
	// unmodified.
	if to == nil {
		return n
	}

	// TODO: This can probably be optimized to not cast as frequently but
	// should otherwise perform just fine.
	if basic, ok := to.Underlying().(*types.Basic); ok {
		switch basic.Kind() {
		case types.Int, types.Int32, types.UntypedInt:
			return &Cast{X: n, To: "int"}
		case types.Int64:
			return &Cast{X: n, To: "int64"}
		case types.Float64, types.UntypedFloat:
			// As a special case, floating point literals just need to contain
			// a decimal point (.) to be interpreted correctly.
			if lit, ok := n.(Literal); ok {
				if !strings.Contains(string(lit), ".") {
					lit += ".0"
				}
				return lit
			}
			return &Cast{X: n, To: "float64"}
		}
	}
	return n
}

// namespaceFor returns the "namespace" for the given package that was
// specified in a gotohelm namespace directive. It defaults to pkg.Name() if
// not specified.
func (t *Transpiler) namespaceFor(pkg *types.Package) string {
	if ns, ok := t.namespaces[pkg]; ok {
		return ns
	}

	namespace := t.namespaceOverride(pkg)

	if namespace == "" {
		// Really bad heuristic to turn versioned modules into combined names to
		// dance around imports of other charts across different versions.
		// e.g. console/v3 (package console) -> consolev3
		namespace = pkg.Name()
		if importName := pkg.Path()[strings.LastIndex(pkg.Path(), "/")+1:]; pkg.Name() != importName {
			namespace += importName
		}
	}

	t.namespaces[pkg] = namespace

	// Check for duplicates.
	for path, inUse := range t.namespaces {
		if path != pkg && inUse == namespace {
			panic(fmt.Sprintf(
				"cross package conflicting namespaces encountered:\n%q -> %q\n%q -> %q",
				path, inUse,
				pkg, inUse,
			))
		}
	}

	return namespace
}

// funcNameFor returns the transpiled "function" name for a given function
// taking into account directives and receivers, if any.
func (t *Transpiler) funcNameFor(fn *types.Func) string {
	if name, ok := t.names[fn]; ok {
		return name
	}

	directives := t.directivesOf(fn)

	fnName := fn.Name()
	if name, ok := directives["name"]; ok {
		fnName = name
	}

	// To not clash with the same method name in the same package
	// which can be declared in multiple struct the package and function
	// name could be separated with the name of the struct type.
	// package example
	//
	// func FunExample() {} => {{- define "example.FunExample" -}}
	//
	// func (e *Example) MethodExample() {} => {{- define "example.Example.MethodExample" -}}
	if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
		// NB: A method may be declared on an alias to a type in the same
		// package (type A = B; func (A) M()). go/types reports the receiver as
		// the *types.Alias, so unalias to get at the defined type it names.
		rtyp := types.Unalias(recv.Type())

		if ptr, ok := rtyp.(*types.Pointer); ok {
			rtyp = types.Unalias(ptr.Elem())
		}

		fnName = fmt.Sprintf("%s.%s", rtyp.(*types.Named).Obj().Name(), fnName)
	}

	t.names[fn] = fnName

	return fnName
}

// omitemptyRespected return true if the `omitempty` JSON tag would be
// respected by [[json.Marshal]] for the given type.
func omitemptyRespected(typ types.Type) bool {
	// An alias is invisible to json.Marshal, so it must be invisible here too.
	// Without this, a field typed as an alias to a basic type would be included
	// in zero values that go's json.Marshal omits.
	typ = types.Unalias(typ)

	switch typ.(type) {
	case *types.Basic, *types.Pointer, *types.Slice, *types.Map:
		return true
	case *types.Named:
		return omitemptyRespected(typ.Underlying())
	default:
		return false
	}
}

type jsonTag struct {
	Name      string
	Inline    bool
	OmitEmpty bool
	OmitZero  bool
}

func parseTag(tag string) jsonTag {
	match := regexp.MustCompile(`json:"([^"]+)"`).FindStringSubmatch(tag)
	if match == nil {
		// Console configuration is defined using yaml annotations
		match = regexp.MustCompile(`yaml:"([^"]+)"`).FindStringSubmatch(tag)
		if match == nil {
			return jsonTag{}
		}
	}

	idx := strings.Index(match[1], ",")
	if idx == -1 {
		idx = len(match[1])
	}

	return jsonTag{
		Name:      match[1][:idx],
		Inline:    strings.Contains(match[1], "inline"),
		OmitEmpty: strings.Contains(match[1], "omitempty"),
		OmitZero:  strings.Contains(match[1], "omitzero"),
	}
}

type structField struct {
	Field *types.Var
	Tag   jsonTag
}

func (f *structField) JSONName() string {
	if f.Tag.Name != "" && f.Tag.Name != "-" {
		return f.Tag.Name
	}
	return f.Field.Name()
}

// JSONOmit returns true if json.Marshal would omit this field. This is
// determined by checking if the field isn't exported or has the `json:"-"`
// tag.
func (f *structField) JSONOmit() bool {
	return f.Tag.Name == "-" || !f.Field.Exported()
}

// JSONInline returns true if this field should be merged with the JSON of it's
// parent rather than being placed within its own key.
func (f *structField) JSONInline() bool {
	// TODO(chrisseto) Should this respect the nonstandard ",inline" tag?
	return f.Field.Embedded() && f.Tag.Name == ""
}

// IncludeInZero returns true if this field would be included in the output
// [[json.Marshal]]'d called with a zero value of this field's parent struct.
func (f *structField) IncludeInZero() bool {
	// TODO(chrisseto): We can start producing more human readable/ergonomic
	// manifests if we process Kubernetes' +optional annotation. This however
	// breaks a lot of our tests as golang's json.Marshal does not respect
	// those annotations. It may be possible to fix by using one of Kubernetes'
	// marshallers?
	if f.JSONOmit() {
		return false
	}
	if f.Tag.OmitZero {
		return false
	}
	if f.Tag.OmitEmpty && omitemptyRespected(f.Field.Type()) {
		return false
	}
	// special-case for modern versions where this isn't included by default
	if types.Unalias(f.Field.Type()).String() == "k8s.io/apimachinery/pkg/apis/meta/v1.Time" {
		return false
	}
	return true
}

func parseDirectives(in string) map[string]string {
	match := directiveRE.FindAllStringSubmatch(in, -1)

	out := map[string]string{}
	for _, m := range match {
		out[m[1]] = m[2]
	}
	return out
}

// importedPackage searches pkg's transitive imports for the one at path.
func importedPackage(pkg *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{}

	for queue := []*types.Package{pkg}; len(queue) > 0; queue = queue[1:] {
		current := queue[0]
		if seen[current] {
			continue
		}
		seen[current] = true

		if current.Path() == path {
			return current
		}

		queue = append(queue, current.Imports()...)
	}

	return nil
}
