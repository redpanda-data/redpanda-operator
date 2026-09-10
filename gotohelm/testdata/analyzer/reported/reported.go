// Package reported holds the constructs the transpiler reports on rather than
// panicking over.
//
// It's deliberately narrow. The rest of the corpus in ../unsupported covers
// far more, but most of those cases still reach a raw panic or an unchecked
// type assertion, which crashes the driver instead of producing a diagnostic.
// Those packages join this one as the remaining panics are converted.
package reported

func Several(x int, b bool, v any, ch chan int) int {
	// NB: Only switches the rewrite declines belong here. analysistest loads
	// its own packages and never runs LoadPackages, so a plain switch would
	// reach the transpiler un-desugared here and be reported, while the same
	// file goes through the rewrites in TestTranspileReportsEveryProblem.
	switch x {
	case 1:
		x = 2
		fallthrough // want `fallthrough is not supported`
	case 2:
		x = 3
	}

	switch x {
	case 1:
		if b {
			break // want `break inside a switch case is not supported`
		}
		x = 4
	}

	// Every rejection in a switch is reported, not just the first.
	switch x {
	case 1:
		if b {
			break // want `break inside a switch case is not supported`
		}
		fallthrough // want `fallthrough is not supported`
	case 2:
		x = 6
	}

	switch v.(type) { // want `type switch statements are not supported`
	case int:
		x = 5
	}

	select { // want `select statements are not supported`
	case <-ch:
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
