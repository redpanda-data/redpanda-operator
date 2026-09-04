// Package second stands in for a chart that bundles first as a subchart and
// declares the same namespace, so their templates would collide.
//
// +gotohelm:namespace=shared
package second

import "github.com/redpanda-data/redpanda-operator/gotohelm/testdata/analyzer/collision/first"

func Name() string { return first.Name() + "second" }
