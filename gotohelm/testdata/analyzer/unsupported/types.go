package unsupported

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Base struct {
	Name string `json:"name"`
}

type Inlined struct {
	Base `json:",inline"`

	Extra string `json:"extra"`
}

type NamedEmbed struct {
	Base `json:"base"`

	Extra string `json:"extra"`
}

type InlinedPointer struct { // want `the embedded field Base is a \*.*Base, not a struct` InlinedPointer:`untranspilable:the embedded field Base`
	*Base

	Extra string `json:"extra"`
}

type InlinedInterface struct { // want `the embedded field error is a error, not a struct` InlinedInterface:`untranspilable:the embedded field error`
	error

	Extra string `json:"extra"`
}

type Marshaling struct { // want `implements json.Marshaler or json.Unmarshaler` Marshaling:`untranspilable:.*implements json.Marshaler`
	Value string
}

func (m Marshaling) MarshalJSON() ([]byte, error) { return nil, nil }

type Wrapper struct { // want `implements json.Marshaler or json.Unmarshaler` Wrapper:`untranspilable:.*implements json.Marshaler`
	Marshaling Marshaling `json:"marshaling"`
}

type Complexes struct { // want `complex128 has no JSON representation` Complexes:`untranspilable:complex128 has no JSON representation`
	Value complex128 `json:"value"`
}

type Arrays struct { // want `arrays are not supported` Arrays:`untranspilable:arrays are not supported`
	Values [3]int `json:"values"`
}

type Channels struct { // want `channels are not supported` Channels:`untranspilable:channels are not supported`
	Values chan int `json:"values"`
}

func SupportedInlining() Inlined {
	var out Inlined
	out.Name = "a"
	return out
}

func SupportedSpecialCases() (metav1.Time, resource.Quantity) {
	var t metav1.Time
	var q resource.Quantity
	return t, q
}

func SupportedRawMessageBehindOmitEmpty() metav1.ObjectMeta {
	var meta metav1.ObjectMeta
	return meta
}

func PromotedThroughNamedEmbed() string {
	var out NamedEmbed
	return out.Name // want `unable to resolve the field "Name"`
}

func InlinedPointerEmbed() InlinedPointer {
	var out InlinedPointer
	return out
}

func InlinedInterfaceEmbed() InlinedInterface {
	var out InlinedInterface
	return out
}

func UnsupportedMarshaler() Wrapper {
	var out Wrapper
	return out
}

// json.RawMessage is declared outside the chart, so there's no fact for it and
// no declaration of ours to report against. It's caught at the use site.
func UnsupportedRawMessage() json.RawMessage {
	var out json.RawMessage // want `implements json.Marshaler or json.Unmarshaler`
	return out
}

func UnsupportedComplex() Complexes {
	var out Complexes
	return out
}

func UnsupportedArray() Arrays {
	var out Arrays
	return out
}

func UnsupportedChannel() Channels {
	var out Channels
	return out
}
