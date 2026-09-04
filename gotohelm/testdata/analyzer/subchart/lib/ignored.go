// +gotohelm:ignore=true // want package:`ignored.go\{ignore=true\}`
package lib

// FileIgnored gets no template: the whole file is skipped.
func FileIgnored() string { // want FileIgnored:`gotohelm:ignore=true`
	return "a"
}
