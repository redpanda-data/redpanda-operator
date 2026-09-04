// Package root stands in for a chart that bundles lib as a subchart.
package root

import "github.com/redpanda-data/redpanda-operator/gotohelm/testdata/analyzer/subchart/lib"

// UsesFine is fine; nothing lib declares here is broken.
func UsesFine(f lib.Fine) string {
	return lib.Upper(f.Describe())
}

// UsesBroken references a type lib's own pass found unusable. The reason
// travels as a fact, so it's reported here too.
func UsesBroken() lib.Broken {
	var out lib.Broken // want `cannot be represented in a helm template`
	return out
}

// CallsBrokenMethod calls a method lib's own pass found untranspilable, so the
// template it would reference is never emitted.
func CallsBrokenMethod(f lib.Fine) int {
	return f.Method(1) // want `is not transpilable, so calling it would reference a template that doesn't exist`
}

// CallsIgnored includes a template that lib never defines.
func CallsIgnored() string {
	return lib.Ignored() // want `no template is emitted for .*, which is annotated with \+gotohelm:ignore=true`
}

// CallsFileIgnored includes a template lib never defines either, though this
// time because the whole file it's declared in is ignored rather than the
// function itself.
func CallsFileIgnored() string {
	return lib.FileIgnored() // want `no template is emitted for .*, which is annotated with \+gotohelm:ignore=true`
}
