package unsupported

func ArrayLiteral() [3]int {
	return [3]int{1, 2, 3} // want `array literals are not supported`
}

func Negation(x int) int {
	return -x // want `negation is only supported on literals`
}

func UnaryPlus(x int) int {
	return +x // want `the unary \+ operator is not supported`
}

func NegativeLiteral() int {
	return -1
}

func MakeMap() map[string]int {
	return make(map[string]int) // want `make is not supported`
}

func MakeSizedMap() map[string]int {
	return make(map[string]int, 10) // want `make is not supported`
}

func MakeSlice() []int {
	return make([]int, 0) // want `make is not supported`
}

func MakeSizedSlice(n int) []int {
	return make([]int, n) // want `the go builtin make is not supported`
}

func New() *int {
	return new(int) // want `the go builtin new is not supported`
}

func Clear(m map[string]int) {
	clear(m) // want `the go builtin clear is not supported`
}

func MinMax(a, b int) int {
	return min(a, b) + max(a, b) // want `the go builtin min is not supported` `the go builtin max is not supported`
}

func Copy(dst, src []int) int {
	return copy(dst, src) // want `the go builtin copy is not supported`
}

func Cap(xs []int) int {
	return cap(xs) // want `the go builtin cap is not supported`
}
