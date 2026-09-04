// Package lib stands in for a subchart bundled into root. Everything it gets
// wrong is reported here, and marked so that root is told at the point of use
// rather than being left to wonder why a template is missing.
package lib

// Broken cannot be represented: gotohelm merges JSON inlined fields into their
// parent, which only works for structs.
type Broken struct { // want `the embedded field error is a error, not a struct` Broken:`untranspilable:the embedded field error`
	error

	Name string `json:"name"`
}

type Fine struct {
	Name string `json:"name"`
}

// Method is a method on Fine that can't be transpiled.
func (Fine) Method(a int) int { // want `a method's receiver must be named` Method:`untranspilable:a method's receiver must be named`
	return a
}

func (f Fine) Describe() string {
	return f.Name
}

// +gotohelm:builtin=upper
func Upper(s string) string { return s } // want Upper:`gotohelm:builtin=upper`

// Ignored is never transpiled, so no template is emitted for it.
//
// +gotohelm:ignore=true
func Ignored() string { // want Ignored:`gotohelm:ignore=true`
	switch 1 {
	case 1:
		return "a"
	}
	return ""
}
