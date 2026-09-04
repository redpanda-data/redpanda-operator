package root

import "github.com/redpanda-data/redpanda-operator/gotohelm/testdata/analyzer/subchart/lib"

// CallsBeforeDeclared appears earlier in the package than nothing in
// particular, but Ignored is declared in a *later* file of lib. The fact has
// to already exist by the time this call is checked.
func CallsBeforeDeclared() string {
	return lib.Ignored() // want `no template is emitted for .*, which is annotated with \+gotohelm:ignore=true`
}

// SameFileForwardReference calls a function declared below it in the same
// file. Merging fact export into the reporting walk would miss this: the fact
// for locallyIgnored wouldn't exist yet when the call is checked.
func SameFileForwardReference() string {
	return locallyIgnored() // want `no template is emitted for .*, which is annotated with \+gotohelm:ignore=true`
}

// +gotohelm:ignore=true
func locallyIgnored() string { // want locallyIgnored:`gotohelm:ignore=true`
	switch 1 {
	case 1:
		return "a"
	}
	return ""
}
