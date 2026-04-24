// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package cluster

import (
	"fmt"
	"reflect"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// SpecConsistencyCheck verifies that the StretchCluster .spec is identical
// across all clusters, mirroring the reconciler's spec drift detection.
type SpecConsistencyCheck struct{}

func (c *SpecConsistencyCheck) Name() string { return "spec-consistency" }

func (c *SpecConsistencyCheck) Run(contexts []*CheckContext) []Result {
	// Collect all clusters that have the StretchCluster.
	var available []*CheckContext
	for _, cc := range contexts {
		if cc.StretchCluster != nil {
			available = append(available, cc)
		}
	}

	if len(available) < 2 {
		return []Result{Skip(c.Name(), "fewer than 2 clusters have the StretchCluster — cannot compare specs")}
	}

	reference := available[0]
	var drifted []string
	for _, cc := range available[1:] {
		if !apiequality.Semantic.DeepEqual(reference.StretchCluster.Spec, cc.StretchCluster.Spec) {
			fields := specDiffFields(reference.StretchCluster.Spec, cc.StretchCluster.Spec)
			drifted = append(drifted, fmt.Sprintf("%s (fields: %s)", cc.Context, strings.Join(fields, ", ")))
		}
	}

	if len(drifted) > 0 {
		return []Result{Fail(c.Name(),
			fmt.Sprintf("spec differs from %s on: %s", reference.Context, strings.Join(drifted, "; ")))}
	}

	return []Result{Pass(c.Name(),
		fmt.Sprintf("specs match across all %d clusters", len(available)))}
}

// specDiffFields returns the top-level JSON field names that differ between
// two StretchClusterSpec values.
func specDiffFields(a, b redpandav1alpha2.StretchClusterSpec) []string {
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	t := av.Type()

	var fields []string
	for i := range t.NumField() {
		if !apiequality.Semantic.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			name := t.Field(i).Name
			if tag, ok := t.Field(i).Tag.Lookup("json"); ok {
				if jsonName, _, _ := strings.Cut(tag, ","); jsonName != "" {
					name = jsonName
				}
			}
			fields = append(fields, name)
		}
	}
	return fields
}
