// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package helmette

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/goutils"
	"github.com/Masterminds/sprig/v3"
	"github.com/imdario/mergo"
	"golang.org/x/exp/maps"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/valuesutil"
)

// ToYAML is the go equivalent of helm's `toYaml`.
//
// Reference
// https://github.com/helm/helm/blob/e90b456d655e78d7c72a32a52a9b70bc1984c33f/pkg/engine/funcs.go#L51
// https://github.com/helm/helm/blob/e90b456d655e78d7c72a32a52a9b70bc1984c33f/pkg/engine/funcs.go#L78-L89
// +gotohelm:builtin=toYaml
func ToYaml(value any) string {
	marshalled, err := yaml.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(marshalled), "\n")
}

// Min is the go equivalent of sprig's `min`
// +gotohelm:builtin=min
func Min(in ...int64) int64 {
	result := int64(math.MaxInt64)
	for _, i := range in {
		if i < result {
			result = i
		}
	}
	return result
}

// First function can not return `T` as sprig implementation
// will return nil if array is of the size 0.
//
// # Reference
// https://github.com/Masterminds/sprig/blob/581758eb7d96ae4d113649668fa96acc74d46e7f/list.go#L161-L163
// +gotohelm:builtin=first
func First[T any](x []T) any {
	if len(x) == 0 {
		return nil
	}

	return x[0]
}

// TrimSuffix is the go equivalent of sprig's `trimSuffix`
// +gotohelm:builtin=trimSuffix
func TrimSuffix(suffix, s string) string {
	if strings.HasSuffix(s, suffix) {
		return strings.TrimSuffix(s, suffix)
	}
	return s
}

// TrimPrefix is the go equivalent of sprig's `trimPrefix`
// +gotohelm:builtin=trimPrefix
func TrimPrefix(prefix, s string) string {
	if strings.HasSuffix(s, prefix) {
		return strings.TrimPrefix(s, prefix)
	}
	return s
}

// SortAlpha is the go equivalent of sprig's `sortAlpha`.
// It mutates the provided slice in place and returns the mutated slice.
// +gotohelm:builtin=sortAlpha
func SortAlpha(x []string) []string {
	sort.Strings(x)
	return x
}

// Printf is the go equivalent of text/templates's `printf`
// +gotohelm:builtin=printf
func Printf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

// Quote is the equivalent of sprig's `quote` - it takes an arbitrary list of arguments
// +gotohelm:builtin=quote
func Quote(vs ...any) string {
	result := make([]string, 0, len(vs))
	for _, v := range vs {
		// Lean on %q: "a double-quoted string safely escaped with Go syntax"
		result = append(result, fmt.Sprintf("%q", ToString(v)))
	}
	return strings.Join(result, " ")
}

// SQuote is the equivalent of sprig's `squote`.
// +gotohelm:builtin=squote
func SQuote(vs ...any) string {
	result := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != nil {
			result = append(result, fmt.Sprintf("'%v'", v))
		}
	}
	return strings.Join(result, " ")
}

// KindOf is the go equivalent of sprig's `kindOf`.
// +gotohelm:builtin=kindOf
func KindOf(v any) string {
	return reflect.TypeOf(v).Kind().String()
}

// KindIs is the go equivalent of sprig's `kindIs`.
// +gotohelm:builtin=kindIs
func KindIs(kind string, v any) bool {
	return KindOf(v) == kind
}

// TypeOf is the go equivalent of sprig's `typeOf`.
// +gotohelm:builtin=typeOf
func TypeOf(v any) string {
	// https://github.com/Masterminds/sprig/blob/581758eb7d96ae4d113649668fa96acc74d46e7f/reflect.go#L18
	return fmt.Sprintf("%T", v)
}

// KindIs is the go equivalent of sprig's `typeIs`.
// +gotohelm:builtin=typeIs
func TypeIs(typ string, v any) bool {
	// https://github.com/Masterminds/sprig/blob/581758eb7d96ae4d113649668fa96acc74d46e7f/reflect.go#L9
	return TypeOf(v) == typ
}

// HasKey is the go equivalent of sprig's `hasKey`.
// +gotohelm:builtin=hasKey
func HasKey[K comparable, V any](m map[K]V, key K) bool {
	_, ok := m[key]
	return ok
}

// Keys is the go equivalent of sprig's `keys`.
// +gotohelm:builtin=keys
func Keys[K comparable, V any](m map[K]V) []K {
	return maps.Keys(m)
}

// Merge is a go equivalent of sprig's `merge`.
func Merge[K comparable, V any](sources ...map[K]V) map[K]V {
	dst := map[K]V{}
	for _, src := range sources {
		if err := mergo.Merge(&dst, src); err != nil {
			return nil
		}
	}
	return dst
}

// MergeTo transpiles to `merge`, but in the golang domain it'll return the
// type requested
func MergeTo[T any](sources ...any) T {
	dst := map[string]any{}
	for _, src := range sources {
		// Turn it into something json-like
		roundTrip, err := valuesutil.RoundTripThrough[any](src)
		if err != nil {
			panic(fmt.Errorf("cannot round trip element: %w", err))
		}

		if err := mergo.Merge(&dst, roundTrip); err != nil {
			panic(fmt.Errorf("cannot merge roundtripped element: %w", err))
		}
	}
	result, err := valuesutil.UnmarshalInto[T](dst)
	if err != nil {
		panic(fmt.Errorf("cannot recast merged structure: %w", err))
	}
	return result
}

// Dig is a go equivalent of sprig's `dig`.
func Dig(m map[string]any, fallback any, path ...string) any {
	val := any(m)

	for _, key := range path {
		var ok bool
		val, ok = val.(map[string]any)
		if !ok {
			return fallback
		}

		val, ok = val.(map[string]any)[key]
		if !ok {
			return fallback
		}
	}

	return val
}

// Trunc is a go equivalent of sprig's `trunc`.
// +gotohelm:builtin=trunc
func Trunc(length int, in string) string {
	if len(in) < length {
		return in
	}
	return in[:length]
}

// Default is a go equivalent of sprig's `default`.
// +gotohelm:builtin=default
func Default[T any](default_, value T) T {
	if Empty(value) {
		return default_
	}
	return value
}

// RegexMatch is the go equivalent of sprig's `regexMatch`.
// +gotohelm:builtin=regexMatch
func RegexMatch(pattern, s string) bool {
	return regexp.MustCompile(pattern).MatchString(s)
}

// MustRegexMatch is the go equivalent of sprig's `mustRegexMatch`.
// +gotohelm:builtin=mustRegexMatch
func MustRegexMatch(pattern, s string) bool {
	return RegexMatch(pattern, s)
}

// RegexSplit is the go equivalent of sprig's `regexSplit`.
// +gotohelm:builtin=mustRegexSplit
func RegexSplit(pattern, s string, n int) []string {
	r := regexp.MustCompile(pattern)
	return r.Split(s, n)
}

// MustRegexSplit is the go equivalent of sprig's `mustRegexSplit`.
// +gotohelm:builtin=mustRegexSplit
func MustRegexSplit(pattern, s string, n int) ([]string, error) {
	r, err := regexp.Compile(pattern)
	if err != nil {
		return []string{}, err
	}
	return r.Split(s, n), nil
}

// +gotohelm:builtin=regexReplaceAll
func RegexReplaceAll(regex, s, repl string) string {
	r := regexp.MustCompile(regex)
	return r.ReplaceAllString(s, repl)
}

// Coalesce is the go equivalent of sprig's `coalesce`.
// +gotohelm:builtin=coalesce
func Coalesce[T any](values ...T) T {
	for _, v := range values {
		if !Empty(v) {
			return v
		}
	}
	var zero T
	return zero
}

// Empty is the go equivalent of sprig's `empty`.
// +gotohelm:builtin=empty
func Empty(value any) bool {
	truthy, ok := template.IsTrue(value)
	if !truthy || !ok {
		return true
	}

	// IsTrue always returns true for structs. So we've got to do some extra
	// work as our structs aren't always structs...
	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Struct:
		return v.IsZero()

	case reflect.Pointer:
		if !v.IsNil() && v.Elem().Kind() == reflect.Struct || v.Elem().Kind() == reflect.String {
			return v.Elem().IsZero()
		}
	}

	return false
}

// Required is the go equivalent of sprig's `required`.
// +gotohelm:builtin=required
func Required(msg string, value any) {
	if Empty(value) {
		Fail(msg)
	}
}

// Fail is the go equivalent of sprig's `fail`.
// +gotohelm:builtin=fail
func Fail(msg string) {
	panic(msg)
}

// ToJSON is the go equivalent of sprig's `toJson`.
// +gotohelm:builtin=toJson
func ToJSON(value any) string {
	// Go's JSON serialization is deterministic. Structs utilize their type
	// information but maps have their keys sorted meaning that a struct's JSON
	// serialization may not match the ordering of it's equivalent map. Most
	// times this doesn't matter but we have some cases where ToJSON is passed
	// to a hashing function.
	// To ensure that values are always equivalent, we first round trip them
	// and then return the re-marshalled JSON.
	value, err := valuesutil.RoundTripThrough[any](value)
	if err != nil {
		return ""
	}

	marshalled, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	return string(marshalled)
}

// MustToJSON is the go equivalent of sprig's `mustToJson`.
// +gotohelm:builtin=mustToJson
func MustToJSON(value any) string {
	marshalled, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(marshalled)
}

// FromJSON is the go equivalent of sprig's `fromJson`.
// +gotohelm:builtin=fromJson
func FromJSON(data string) any {
	var out any
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return ""
	}
	return out
}

// FromYaml is the go equivalent of gohelm's wrapper around helm's `fromYaml`
// Rather than silently failing, FromYaml panics when Unmarshaling fails.
// WARNING: In helm world, T is largely ignored. Calls must ensure data is well
// formed.
func FromYaml[T any](input string) T {
	// See also: bootstrap.go's FromYaml for the actual wrapper implementation.
	var out T
	if err := yaml.Unmarshal([]byte(input), &out); err != nil {
		panic(err)
	}
	return out
}

// MustFromJSON is the go equivalent of sprig's `mustFromJson`.
// +gotohelm:builtin=mustFromJson
func MustFromJSON(data string) any {
	var out any
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		panic(err)
	}
	return out
}

// Lower is the go equivalent of sprig's `lower`.
// +gotohelm:builtin=lower
func Lower(in string) string {
	return strings.ToLower(in)
}

// Upper is the go equivalent of sprig's `upper`.
// +gotohelm:builtin=upper
func Upper(in string) string {
	return strings.ToUpper(in)
}

// Unset is the go equivalent of sprig's `unset`.
// +gotohelm:builtin=unset
func Unset[K comparable, V any](d map[K]V, key K) {
	delete(d, key)
}

// Until is the go equivalent of spring's `until`.
// There might be better ways to do things with gotohelm, but this
// represents a high-fidelity way to translate templates.
// +gotohelm:builtin=until
func Until(n int) []int {
	result := make([]int, 0, n)
	for i := range n {
		result = append(result, i)
	}
	return result
}

// Concat is the go equivalent of sprig's `concat`.
// +gotohelm:builtin=concat
func Concat[T any](lists ...[]T) []T {
	var out []T
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// Atoi is the go equivalent of sprig's `atoi`.
// +gotohelm:builtin=atoi
func Atoi(in string) (int, error) {
	return strconv.Atoi(in)
}

// +gotohelm:builtin=float64
func Float64(in string) (float64, error) {
	return strconv.ParseFloat(in, 64)
}

// +gotohelm:builtin=len
func Len(in any) int {
	return reflect.ValueOf(in).Len()
}

// +gotohelm:builtin=toString
func ToString(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// +gotohelm:builtin=semverCompare
func SemverCompare(constraint, version string) (bool, error) {
	fn := sprig.FuncMap()["semverCompare"].(func(string, string) (bool, error))
	return fn(constraint, version)
}

// +gotohelm:builtin=join
func Join[T any](sep string, s []T) string {
	out := ""
	for i, el := range s {
		if i > 0 {
			out += sep
		}
		out += ToString(el)
	}
	return out
}

// +gotohelm:builtin=sha256sum
func Sha256Sum(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// +gotohelm:builtin=contains
func Contains(substr, s string) bool {
	return strings.Contains(s, substr)
}

// +gotohelm:builtin=indent
func Indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces)
	return pad + strings.ReplaceAll(v, "\n", "\n"+pad)
}

// +gotohelm:builtin=nindent
func NIndent(spaces int, v string) string {
	return "\n" + Indent(spaces, v)
}

// +gotohelm:builtin=randAlphaNum
func RandAlphaNum(length int) string {
	// Implementation taken from sprig, which uses their own helper library.
	// It is not possible, it appears, to actually generate an error here.
	r, _ := goutils.CryptoRandomAlphaNumeric(length)
	return r
}

// +gotohelm:builtin=replace
func Replace(old, new, s string) string {
	return strings.ReplaceAll(s, old, new)
}

// SortedKeys is a convenience function to aid in iterating over maps in a
// deterministic order.
func SortedKeys[T any](m map[string]T) []string {
	keys := Keys(m)
	return SortAlpha(keys)
}

// Get is a wrapper around sprig's `get` function that returns (nil, false)
// instead of an empty string when the key is missing.
func Get[T any](value any, key string) (T, bool) {
	var zero T // Helper for ok == false
	v := reflect.ValueOf(value)

	if v.Type().Kind() == reflect.Map {
		item := v.MapIndex(reflect.ValueOf(key))
		if item.IsZero() {
			return zero, false
		}
		return item.Interface().(T), true
	}

	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)

		fieldName := field.Name
		if jsonTag, ok := field.Tag.Lookup("json"); ok {
			fieldName = strings.Split(jsonTag, ",")[0]
		}

		if fieldName != key {
			continue
		}

		fieldValue := v.Field(i)

		switch value := fieldValue.Interface().(type) {
		case *T:
			if value == nil {
				return zero, false
			}
			return *value, true
		case T:
			return value, true
		default:
			return zero, false
		}
	}

	return zero, false
}
