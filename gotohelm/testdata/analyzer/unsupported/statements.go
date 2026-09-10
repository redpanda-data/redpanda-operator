package unsupported

// NB: A switch expression has no fixture here. It's desugared into an if else
// chain before transpilation, and analysistest loads its own packages without
// going through LoadPackages, so one written here would be analyzed as a
// switch and reported. The forms the rewrite declines are below.

func Fallthrough(x int) int {
	switch x {
	case 1:
		fallthrough // want `fallthrough is not supported`
	case 2:
		return 2
	}
	return 0
}

func BreakInCase(x int, b bool) int {
	switch x {
	case 1:
		if b {
			break // want `break inside a switch case is not supported`
		}
		return 1
	}
	return 0
}

func TypeSwitch(x any) int {
	switch x.(type) { // want `type switch statements are not supported`
	case int:
		return 1
	}
	return 0
}

func Select(ch chan int) {
	select { // want `select statements are not supported`
	case <-ch:
	}
}

func Go(f func()) {
	go f() // want `go statements are not supported`
}

func Defer(f func()) {
	defer f() // want `defer statements are not supported`
}

func Send(ch chan int) {
	ch <- 1 // want `channel sends are not supported`
}

func Labeled(xs []int) {
outer: // want `labeled statements are not supported`
	for _, x := range xs {
		for range xs {
			if x == 1 {
				break outer // want `labeled break is not supported`
			}
		}
	}
}

func Goto() int {
	goto done // want `goto statements are not supported`
done: // want `labeled statements are not supported`
	return 1
}

func LocalType() int {
	type local int // want `type declarations are not supported inside a function body`
	return int(local(1))
}

func LocalConst() int {
	const x = 1 // want `const declarations are not supported inside a function body`
	return x
}

func BitwiseAssign(x int) int {
	x |= 1 // want `\|= is not supported; helm templates have no bitwise operators`
	return x
}

func SelectorMultiValue(m map[string]int) bool {
	var out struct {
		Value int
		OK    bool
	}
	ok := false
	out.Value, ok = m["key"] // want `multi-value assignments may only assign to plain identifiers`
	return ok
}

// A non-assignment init isn't hoistable, so it's transpiled inline ahead of
// the if. That works.
func IfInitExpr(f func() bool, cond bool) bool {
	if f(); cond {
		return true
	}
	return false
}

func IfInitSelector(m map[string]int) bool {
	var out struct {
		Value int
		OK    bool
	}
	ok := false
	if out.Value, ok = m["key"]; ok { // want `multi-value assignments may only assign to plain identifiers`
		return true
	}
	return false
}

func RangeOverInt() int {
	total := 0
	for range 10 { // want `ranging over int is not supported`
		total = total + 1
	}
	return total
}

func RangeOverChannel(ch chan int) int {
	total := 0
	for range ch { // want `ranging over a channel is not supported`
		total = total + 1
	}
	return total
}
