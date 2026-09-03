// Package reported holds the constructs the transpiler reports on rather than
// panicking over.
//
// It's deliberately narrow. The rest of the corpus in ../unsupported covers
// far more, but most of those cases still reach a raw panic or an unchecked
// type assertion, which crashes the driver instead of producing a diagnostic.
// Those packages join this one as the remaining panics are converted.
package reported

func Several(x int, b bool, v any) int {
	switch x { // want `unhandled ast\.Stmt`
	case 1:
		x = 2
	}

	x += 1 // want `Unsupported assignment token`
	x |= 2 // want `Unsupported assignment token`

	for i := 0; i != x; i++ { // want `of != is not supported in for condition`
		x = x + i
	}

	_ = v.(int) // want `type assertions on numeric types are unreliable`
	_ = uint(x) // want `unsupported type cast to uint`
	_ = x & 3   // want `No matching .* signature`

	return x
}
