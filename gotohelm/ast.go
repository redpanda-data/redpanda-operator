// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:errcheck,gosec
package gotohelm

import (
	"fmt"
	"io"
	"strconv"
)

type Node interface {
	Write(io.Writer)
}

type Until struct {
	Expr Node
}

func (u *Until) Write(w io.Writer) {
	w.Write([]byte("until ("))
	u.Expr.Write(w)
	w.Write([]byte("|int)"))
}

type UntilStep struct {
	Start Node
	Stop  Node
	Step  Node
}

func (u *UntilStep) Write(w io.Writer) {
	w.Write([]byte("untilStep ("))
	u.Start.Write(w)
	w.Write([]byte("|int)"))
	w.Write([]byte(" ("))
	u.Stop.Write(w)
	w.Write([]byte("|int)"))
	w.Write([]byte(" ("))
	u.Step.Write(w)
	w.Write([]byte("|int)"))
}

type ParenExpr struct {
	Expr Node
}

func (s *ParenExpr) Write(w io.Writer) {
	w.Write([]byte("("))
	s.Expr.Write(w)
	w.Write([]byte(")"))
}

type Selector struct {
	Expr  Node
	Field string
	// Inlined indicates if `Field` is a JSON inlined (embedded) field or not.
	Inlined bool
}

func (s *Selector) Write(w io.Writer) {
	s.Expr.Write(w)
	// If this Selector is referencing an inlined field, don't emit it as
	// gotohelm's "object model" is the JSON representation of structs, not
	// go's representation.
	if !s.Inlined {
		fmt.Fprintf(w, ".%s", s.Field)
	}
}

type Nil struct{}

func (*Nil) Write(w io.Writer) {
	// nil is strange for some reason, in many cases it's acceptable to just
	// have `nil` but in others, you'll get `nil is not a command` errors.
	// {{ $_ := nil }} Doesn't work
	// {{ $_ := (eq nil nil) }} Works
	// It's too difficult to inspect all the cases and use nil in some but not
	// others, instead wrap nil in a function that just returns nil.
	// (fromJSON "null") doesn't work quite as expected but coalesce seems to
	// do the trick.
	w.Write([]byte(`(coalesce nil)`))
}

type Statement struct {
	NoCapture bool
	Expr      Node
}

func (s *Statement) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- ")
	if !s.NoCapture {
		fmt.Fprintf(w, "$_ := ")
	}
	s.Expr.Write(w)
	fmt.Fprintf(w, " -}}\n")
}

type Binary struct {
	LHS Node
	Op  string
	RHS Node
}

func (b *Binary) Write(w io.Writer) {
	b.LHS.Write(w)
	fmt.Fprintf(w, " %s ", b.Op)
	b.RHS.Write(w)
}

type Ident struct {
	Name string
}

func (i *Ident) Write(w io.Writer) {
	fmt.Fprintf(w, "$%s", i.Name)
}

type BuiltInCall struct {
	Func      Node
	Arguments []Node
}

func (c *BuiltInCall) Write(w io.Writer) {
	fmt.Fprintf(w, "(")
	c.Func.Write(w)
	for _, arg := range c.Arguments {
		fmt.Fprintf(w, " ")
		arg.Write(w)
	}
	fmt.Fprintf(w, ")")
}

type Cast struct {
	To string
	X  Node
}

func (c *Cast) Write(w io.Writer) {
	fmt.Fprintf(w, "(")
	c.X.Write(w)
	fmt.Fprintf(w, " | %s)", c.To)
}

type Call struct {
	FuncName     Node
	Encapsulator Node
	Arguments    []Node
}

func litCall(funcName string, args ...Node) *Call {
	return &Call{FuncName: Quoted(funcName), Arguments: args}
}

func (c *Call) Write(w io.Writer) {
	encapsulator := c.Encapsulator
	if encapsulator == nil {
		encapsulator = Literal("list")
	}

	args := &DictLiteral{
		KeysValues: []*KeyValue{
			{
				Key: Quoted("a"),
				Value: &BuiltInCall{
					Func:      encapsulator,
					Arguments: c.Arguments,
				},
			},
		},
	}

	fmt.Fprintf(w, `(get (fromJson (include `)
	c.FuncName.Write(w)
	fmt.Fprintf(w, ` `)
	args.Write(w)
	fmt.Fprintf(w, `)) %q)`, "r")
}

type Assignment struct {
	LHS Node
	New bool
	RHS Node
}

func (a *Assignment) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- ")
	a.LHS.Write(w)
	fmt.Fprintf(w, " ")
	if a.New {
		fmt.Fprintf(w, ":")
	}
	fmt.Fprintf(w, "= ")
	a.RHS.Write(w)
	fmt.Fprintf(w, " -}}\n")
}

type DictLiteral struct {
	KeysValues []*KeyValue
}

func (d *DictLiteral) Write(w io.Writer) {
	fmt.Fprintf(w, "(dict")
	for _, p := range d.KeysValues {
		fmt.Fprintf(w, " ")
		p.Write(w)
	}
	fmt.Fprintf(w, ")")
}

type KeyValue struct {
	Key   Node
	Value Node
}

func (p *KeyValue) Write(w io.Writer) {
	p.Key.Write(w)
	w.Write([]byte{' '})
	p.Value.Write(w)
}

type File struct {
	Source string
	Name   string
	Header string
	Funcs  []*Func
	Footer string
}

func (f *File) Write(w io.Writer) {
	if f.Source != "" {
		fmt.Fprintf(w, "{{- /* GENERATED FILE DO NOT EDIT */ -}}\n")
		fmt.Fprintf(w, "{{- /* Transpiled by gotohelm from %q */ -}}\n\n", f.Source)
	}
	w.Write([]byte(f.Header))
	for _, s := range f.Funcs {
		s.Write(w)
		w.Write([]byte{'\n'})
	}
	w.Write([]byte(f.Footer))
}

type Func struct {
	Namespace  string
	Name       string
	Params     []Node
	Statements []Node
}

func (f *Func) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- define %q -}}\n", f.Namespace+"."+f.Name)
	for i := range f.Params {
		fmt.Fprintf(w, "{{- ")
		f.Params[i].Write(w)
		fmt.Fprintf(w, " := (index .a %d) -}}\n", i)
	}
	fmt.Fprintf(w, "{{- range $_ := (list 1) -}}\n")
	fmt.Fprintf(w, "{{- $_is_returning := false -}}\n")
	for _, s := range f.Statements {
		s.Write(w)
	}
	fmt.Fprintf(w, "{{- end -}}\n")
	fmt.Fprintf(w, "{{- end -}}\n")
}

type Return struct {
	Expr Node
}

func (r *Return) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- $_is_returning = true -}}\n")
	fmt.Fprintf(w, "{{- (dict %q ", "r")
	r.Expr.Write(w)
	fmt.Fprintf(w, ") | toJson -}}\n")
	fmt.Fprintf(w, "{{- break -}}\n")
}

type Literal string

func Quoted(unquoted string) Literal {
	return Literal(strconv.Quote(unquoted))
}

func (l Literal) Write(w io.Writer) {
	fmt.Fprintf(w, "%s", l)
}

// Block is a bare sequence of statements that introduces no scope of its own.
//
// It's the body of anything that already scopes -- an if, a range, a function --
// and the group of statements a single go statement can expand into. For a go
// block statement, which does scope, see [Scope].
type Block struct {
	Statements []Node
}

func (b *Block) Write(w io.Writer) {
	for _, s := range b.Statements {
		s.Write(w)
	}
}

// Scope is a go block statement.
//
//	x := 1
//	{
//		x := 2
//	}
//	// x is 1
//
// Templates have no block, so it's borrowed from an `if` that's always true:
// text/template pops the variable stack at the end of an if body. `with` pops
// too but rebinds dot, and `range $_ := (list 1)`, which [Func] uses, swallows
// break and continue, so either one in the block would stop targeting the go
// loop it belongs to.
type Scope struct {
	Statements []Node
}

func (s *Scope) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- if true -}}\n")
	for _, stmt := range s.Statements {
		stmt.Write(w)
	}
	fmt.Fprintf(w, "{{- end -}}\n")
}

type Range struct {
	Key   Node
	Value Node
	Over  Node
	Body  Node
}

func (r *Range) Write(w io.Writer) {
	fmt.Fprintf(w, "{{- range ")
	if r.Key != nil {
		r.Key.Write(w)
	} else {
		w.Write([]byte("$_"))
	}
	fmt.Fprintf(w, ", ")
	if r.Value != nil {
		r.Value.Write(w)
	} else {
		w.Write([]byte("$_"))
	}
	fmt.Fprintf(w, " := ")
	r.Over.Write(w)
	fmt.Fprintf(w, " -}}\n")
	r.Body.Write(w)
	fmt.Fprintf(w, "{{- end -}}\n")
	fmt.Fprintf(w, "{{- if $_is_returning -}}\n")
	fmt.Fprintf(w, "{{- break -}}\n")
	fmt.Fprintf(w, "{{- end -}}\n")
}

type IfStmt struct {
	Init Node
	Cond Node
	Body Node
	Else Node
}

func (i *IfStmt) Write(w io.Writer) {
	if i.Init != nil {
		i.Init.Write(w)
	}

	fmt.Fprintf(w, "{{- if ")
	i.Cond.Write(w)
	fmt.Fprintf(w, " -}}\n")

	if i.Body != nil {
		i.Body.Write(w)
	}

	if i.Else != nil {
		fmt.Fprintf(w, "{{- else -}}")
		if _, ok := i.Else.(*IfStmt); !ok {
			fmt.Fprintf(w, "\n")
		}
		i.Else.Write(w)
	}

	fmt.Fprintf(w, "{{- end -}}\n")
}

// Invalid stands in for a node that couldn't be transpiled.
//
// [Transpiler.report] returns it so that a rule can bail out of one expression
// or statement without unwinding the whole walk, which is what lets a single
// pass report every problem in a package instead of only the first.
//
// NB: It is deliberately not nil. nil already means "absent" throughout the
// transpiler -- a slice expression with no low bound, a range with no key --
// and consumers substitute defaults for it. Reusing nil for "failed" would let
// a reported error be silently replaced by a plausible looking default.
type Invalid struct{}

func (*Invalid) Write(io.Writer) {
	// Transpiled output is discarded whenever anything was reported, so
	// reaching this means a caller used a chart it was told not to.
	panic("gotohelm: transpiled output used despite reported diagnostics")
}
