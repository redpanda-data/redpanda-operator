package unsupported

func LiteralBound() int {
	total := 0
	for i := 0; i < 10; i++ { // want `a for loop's bounds must be an identifier or a field selector`
		total = total + i
	}
	return total
}

func CallBound(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ { // want `a for loop's bounds must be an identifier or a field selector`
		total = total + xs[i]
	}
	return total
}

func VarDeclaredBound(n int) int {
	var limit = n
	total := 0
	for i := 0; i < limit; i++ { // want `a for loop's bounds must be declared with .:=. or be a function parameter`
		total = total + i
	}
	return total
}

func UnsupportedOp(n int) int {
	total := 0
	for i := 0; i != n; i++ { // want `!= is not supported in a for condition`
		total = total + i
	}
	return total
}

func Forever() int {
	total := 0
	for { // want `for loops must have a condition of the form`
		total = total + 1
	}
}

func WhileLoop(cond bool) int {
	total := 0
	for cond { // want `for loop conditions must be a comparison of the form`
		total = total + 1
	}
	return total
}

func NoInit(n int) int {
	i := 0
	total := 0
	for ; i < n; i++ { // want `for loops must initialize their loop variable`
		total = total + i
	}
	return total
}

func BinaryPost(n int) int {
	total := 0
	for i := 0; i < n; i = i + 1 { // want `a for loop's post statement must be`
		total = total + i
	}
	return total
}

func NoPost(n int) int {
	total := 0
	for i := 0; i < n; { // want `for loops must have a post statement of the form`
		total = total + i
	}
	return total
}

func SupportedLoop(n int) int {
	total := 0
	for i := 0; i < n; i++ {
		total = total + i
	}
	for i := 0; i < n; i += 2 {
		total = total + i
	}
	for i := n; i > n; i-- {
		total = total + i
	}
	return total
}
