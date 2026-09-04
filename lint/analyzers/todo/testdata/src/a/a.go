package a

// TODO tidy this up // want "without an owner or issue"
func Anonymous() {}

// TODO(pawel) tidy this up
func Owned() {}

// FIXME(#1234) this breaks on leap years
func Ticketed() {}

func Inline() {
	x := 1 // FIXME later // want "without an owner or issue"
	_ = x
}
