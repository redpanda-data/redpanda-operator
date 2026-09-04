package unsupported

import "github.com/redpanda-data/redpanda-operator/gotohelm/helmette"

func Sorted(m map[string]int) []int {
	var out []int
	for _, v := range helmette.SortedMap(m) {
		out = append(out, v)
	}
	return out
}

func GroupedVars() int {
	var ( // want `grouped var declarations are not supported`
		a = 1
		b = 2
	)
	return a + b
}

func MultipleNames() int {
	var a, b = 1, 2 // want `var declarations may only declare a single variable`
	return a + b
}

func CompoundAssign(x int) int {
	x += 1 // want `\+= is not supported`
	x -= 2 // want `-= is not supported`
	x *= 3 // want `\*= is not supported`
	x /= 4 // want `/= is not supported`
	x %= 5 // want `%= is not supported`
	return x
}

func IncDec(x int) int {
	x++ // want `\+\+ is only supported as a for loop's post statement`
	x-- // want `-- is only supported as a for loop's post statement`
	return x
}

func MapMutation(m map[string]int) []int {
	var out []int
	for _, v := range m { // want `ranges over maps are non-deterministic`
		out = append(out, v)
	}
	return out
}
