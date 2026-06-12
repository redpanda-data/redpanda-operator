// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestClusterSourcePredicate pins the event-filtering contract that
// ClusterSourceWatchOptions relies on: status-only updates of a cluster CR
// (resourceVersion bumped, generation unchanged) must not re-enqueue
// referencing resources, while spec changes, creations, and deletions must.
func TestClusterSourcePredicate(t *testing.T) {
	cluster := func(generation int64, resourceVersion string) *redpandav1alpha2.Redpanda {
		return &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "redpanda",
				Namespace:       "redpanda",
				Generation:      generation,
				ResourceVersion: resourceVersion,
			},
		}
	}

	require.False(t, ClusterSourcePredicate.Update(event.UpdateEvent{
		ObjectOld: cluster(1, "100"),
		ObjectNew: cluster(1, "101"),
	}), "status-only update (generation unchanged) should be filtered out")

	require.True(t, ClusterSourcePredicate.Update(event.UpdateEvent{
		ObjectOld: cluster(1, "100"),
		ObjectNew: cluster(2, "101"),
	}), "spec update (generation bumped) should pass")

	require.True(t, ClusterSourcePredicate.Create(event.CreateEvent{
		Object: cluster(1, "100"),
	}), "create events should pass")

	require.True(t, ClusterSourcePredicate.Delete(event.DeleteEvent{
		Object: cluster(1, "100"),
	}), "delete events should pass")
}

func TestClusterSourceWatchOptions(t *testing.T) {
	require.Len(t, ClusterSourceWatchOptions(), 1,
		"ClusterSourceWatchOptions should carry exactly the predicate option")
}
