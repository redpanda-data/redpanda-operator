// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build integration

package redpanda_test

import (
	"fmt"
	"sort"
	"strings"
	"time"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/stretchr/testify/assert"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestStableUIDAndGeneration asserts that UIDs, Generations, Labels, and
// Annotations of all objects created by the controller are stable across flux
// and de-fluxed.
func (s *RedpandaControllerSuite) TestIntegrationStableUIDAndGeneration() {
	s.T().Skip("Too flaky")
	testutil.SkipIfNotIntegration(s.T())
	testutil.RequireTimeout(s.T(), time.Minute*20)

	isStable := func(a, b client.Object) {
		assert.Equal(s.T(), a.GetUID(), b.GetUID(), "%T %q's UID changed (Something recreated it)", a, a.GetName())
		assert.Equal(s.T(), a.GetLabels(), b.GetLabels(), "%T %q's Labels changed", a, a.GetName())
		assert.Equal(s.T(), a.GetAnnotations(), b.GetAnnotations(), "%T %q's Labels changed", a, a.GetName())
		assert.Equal(s.T(), a.GetGeneration(), b.GetGeneration(), "%T %q's Generation changed (Something changed .Spec)", a, a.GetName())
	}

	// A loop makes this easier to maintain but not to read. We're testing that
	// the following paths from "fresh" hold the isStable property defined
	// above.
	// - NoFlux (Fresh) -> Flux (Toggled) -> NoFlux (Toggled)
	// - Flux (Fresh) -> NoFlux (Toggled) -> Flux (Toggled)
	for _, useFlux := range []bool{true, false} {
		rp := s.minimalRP(useFlux)

		s.applyAndWait(rp)

		filter := client.MatchingLabels{"app.kubernetes.io/instance": rp.Name}

		fresh := s.snapshotCluster(filter)

		rp.Spec.ChartRef.UseFlux = ptr.To(!useFlux)
		s.applyAndWait(rp)

		flipped := s.snapshotCluster(filter)
		s.compareSnapshot(fresh, flipped, isStable)

		rp.Spec.ChartRef.UseFlux = ptr.To(useFlux)
		s.applyAndWait(rp)

		flippedBack := s.snapshotCluster(filter)
		s.compareSnapshot(flipped, flippedBack, isStable)

		s.deleteAndWait(rp)
	}
}

func (s *RedpandaControllerSuite) snapshotCluster(opts ...client.ListOption) []kube.Object {
	var objs []kube.Object

	for _, t := range redpandachart.Types() {
		gvk, err := s.client.GroupVersionKindFor(t)
		s.NoError(err)
		if gvk.Group == "batch" && gvk.Version == "v1" && strings.EqualFold(gvk.Kind, "Job") {
			// ignore jobs since those differ between chart installation and direct object
			// creation
			continue
		}

		gvk.Kind += "List"

		list, err := s.client.Scheme().New(gvk)
		s.NoError(err)

		if err := s.client.List(s.ctx, list.(client.ObjectList), opts...); err != nil {
			if apimeta.IsNoMatchError(err) {
				s.T().Logf("skipping unknown list type %T", list)
				continue
			}
			s.NoError(err)
		}

		s.NoError(apimeta.EachListItem(list, func(o runtime.Object) error {
			obj := o.(client.Object)
			obj.SetManagedFields(nil)
			objs = append(objs, obj)
			return nil
		}))
	}

	return objs
}

func (s *RedpandaControllerSuite) compareSnapshot(a, b []client.Object, fn func(a, b client.Object)) {
	getGVKName := func(o client.Object) string {
		gvk, err := s.client.GroupVersionKindFor(o)
		s.NoError(err)
		return gvk.String() + client.ObjectKeyFromObject(o).String()
	}

	groupedA := mapBy(a, getGVKName)
	groupedB := mapBy(b, getGVKName)

	groupedANames := sortedKeyStrings(groupedA)
	groupedBNames := sortedKeyStrings(groupedB)

	assert.JSONEq(s.T(), groupedANames, groupedBNames)

	for key, a := range groupedA {
		b := groupedB[key]
		fn(a, b)
	}
}

func mapBy[T any, K comparable](items []T, fn func(T) K) map[K]T {
	out := make(map[K]T, len(items))
	for _, item := range items {
		key := fn(item)
		if _, ok := out[key]; ok {
			panic(fmt.Sprintf("duplicate key: %v", key))
		}
		out[key] = item
	}
	return out
}

func sortedKeyStrings[T any, K ~string](items map[K]T) string {
	var keys sort.StringSlice
	for key := range items {
		keys = append(keys, "\""+string(key)+"\"")
	}
	keys.Sort()

	return fmt.Sprintf("[%s]", strings.Join(keys, ", "))
}
