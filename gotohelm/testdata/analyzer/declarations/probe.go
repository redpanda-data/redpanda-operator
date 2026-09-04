package declarations

// Shadowed is a package level function carrying a builtin directive whose name
// is also used by a method. Resolving the directive must distinguish the two.
//
// +gotohelm:builtin=upper
func Shadowed(s string) string { return s } // want Shadowed:`gotohelm:builtin=upper`

type Box struct{ V string }

// Shadowed has no directive, so its (string, int) signature is fine.
func (b Box) Shadowed() (string, int) { return b.V, 1 }

func CallBoth(b Box, s string) string {
	_ = Shadowed(s)
	out, _ := b.Shadowed()
	return out
}
