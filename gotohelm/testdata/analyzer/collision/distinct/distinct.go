// Package distinct bundles first as a subchart but keeps its own namespace, so
// there's nothing to collide.
package distinct

import "github.com/redpanda-data/redpanda-operator/gotohelm/testdata/analyzer/collision/first"

func Name() string { return first.Name() + "distinct" }
