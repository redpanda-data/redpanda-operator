package unsupported

type Named struct {
	Field string
}

type Reader interface {
	Read() string
}

var PackageLevel = "value"

func Closure() func() int {
	return func() int { return 1 } // want `function literals are not supported`
}

func ChannelReceive(ch chan int) int {
	return <-ch // want `channel receives are not supported`
}

func TypeAsValue() string {
	return Named{Field: "a"}.Field
}

func PackageVariable() string {
	return PackageLevel // want `the package level variable PackageLevel is not supported`
}

func Bitwise(a, b int) int {
	return a & b // want `& is not supported; helm templates have no bitwise operators`
}

func Shift(a int) int {
	return a << 2 // want `<< is not supported; helm templates have no bitwise operators`
}

func MixedWidths(a int, b int64) int64 {
	return int64(a) + b
}

func UnsignedArithmetic(a, b uint) uint {
	return a + b // want `\+ is not supported between uint and uint`
}

func NarrowArithmetic(a, b int8) int8 {
	return a + b // want `\+ is not supported between int8 and int8`
}

func NumericAssertion(x any) int {
	return x.(int) // want `type assertions on numeric types are unreliable`
}

func NamedAssertion(x any) Named {
	return x.(Named) // want `cannot assert to the named type`
}

func InterfaceAssertion(x any) Reader {
	return x.(Reader) // want `cannot assert to the interface`
}

func SupportedAssertions(x any) (string, map[string]any, []any, bool) {
	s := x.(string)
	m := x.(map[string]any)
	l := x.([]any)
	_, ok := x.(string)
	return s, m, l, ok
}

func ByteSliceConversion(s string) []byte {
	return []byte(s) // want `conversion to \[\]byte is not supported`
}

func UnsignedConversion(i int) uint {
	return uint(i) // want `conversion to uint is not supported`
}

func Float32Conversion(i int) float32 {
	return float32(i) // want `conversion to float32 is not supported`
}

func InterfaceConversion(n Named) Reader {
	return Reader(n) // want `conversion to .*Reader is not supported`
}

func (n Named) Read() string { return n.Field }

type Key struct{ Name string }

func StructMapKeys(k Key) map[Key]string {
	return map[Key]string{k: "a"} // want `map keys must be assignable or convertible to string`
}

func UnkeyedStruct() Named {
	return Named{"a"} // want `struct literals must name their fields`
}

func EmptyStruct() struct{} {
	return struct{}{} // want `unable to resolve the definition of a struct without at least one field`
}

// The comma-ok form has the same hazard as the single value form: every number
// is a float64 by the time it reaches a template, so the check never passes.
func NumericAssertionCommaOK(x any) (int, bool) {
	i, ok := x.(int) // want `type assertions on numeric types are unreliable`
	return i, ok
}
