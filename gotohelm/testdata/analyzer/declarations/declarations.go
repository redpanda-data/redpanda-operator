// +gotohelm:namespace=declarations // want package:`declarations.go\{namespace=declarations\} namespace.go\{namespace=other\}`
package declarations

type Widget struct {
	Name string `json:"name"`
}

type Alias = Widget

// UnnamedParams are harmless; the body can't reference them and callers just
// pass an argument the template ignores. Go forbids mixing them with named
// parameters, so they can never shift a binding.
func UnnamedParams(int, int) int {
	return 0
}

// BlankParams are fine; the transpiler binds them to $_.
func BlankParams(a int, _ string, c int) int {
	return a + c
}

// UnnamedReceiver shifts every parameter of the method by one.
func (Widget) UnnamedReceiver(a int) int { // want `a method's receiver must be named` UnnamedReceiver:`untranspilable:a method's receiver must be named`
	return a
}

// ParameterlessUnnamedReceiver is harmless; there's nothing to shift.
func (Widget) ParameterlessUnnamedReceiver() int {
	return 1
}

// Renamed is transpiled under a different template name.
//
// +gotohelm:name=Original
func Renamed() int { return 1 } // want Renamed:`gotohelm:name=Original`

// Collides transpiles to the same template name as Renamed.
//
// +gotohelm:name=Original
func Collides() int { // want `transpiles to the same template name` Collides:`gotohelm:name=Original`
	return 2
}

// Ignored is never transpiled, so its contents are unconstrained.
//
// +gotohelm:ignore=true
func Ignored() int { // want Ignored:`gotohelm:ignore=true`
	switch 1 {
	case 1:
		return 1
	}
	return 0
}

// Builtin stands in for a template builtin, so its body is never transpiled.
//
// +gotohelm:builtin=lower
func Builtin(s string) string { // want Builtin:`gotohelm:builtin=lower`
	switch s {
	case "":
		return ""
	}
	return s
}

func (w Widget) Name2() string { return w.Name }

func (a Alias) Name3() string { return a.Name }

// SingleResultBuiltin is fine; a builtin may return one value.
//
// +gotohelm:builtin=lower
func SingleResultBuiltin(s string) string { return s } // want SingleResultBuiltin:`gotohelm:builtin=lower`

// ErrorPairBuiltin is fine; gotohelm wraps the call so it looks like it
// returns an error, even though template execution just halts.
//
// +gotohelm:builtin=mustFromJson
func ErrorPairBuiltin(s string) (string, error) { return s, nil } // want ErrorPairBuiltin:`gotohelm:builtin=mustFromJson`

// CommaOkBuiltin has no equivalent; a template builtin can't return two real
// values.
//
// +gotohelm:builtin=lower
func CommaOkBuiltin(s string) (string, bool) { return s, true } // want CommaOkBuiltin:`gotohelm:builtin=lower`

func CallCommaOkBuiltin(s string) string {
	out, _ := CommaOkBuiltin(s) // want `is annotated with \+gotohelm:builtin=lower but returns 2 values`
	return out
}

func CallSupportedBuiltins(s string) (string, string) {
	a := SingleResultBuiltin(s)
	b, _ := ErrorPairBuiltin(s)
	return a, b
}

// CallIgnored hits the same rule within a single package; the directive is
// read from the fact this pass exported for Ignored a moment ago.
func CallIgnored() int {
	return Ignored() // want `no template is emitted for .*, which is annotated with \+gotohelm:ignore=true`
}
