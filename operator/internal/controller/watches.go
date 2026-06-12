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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ClusterSourcePredicate filters the events of cluster CR watches down to
// spec changes (and Create/Delete). Cluster CRs receive frequent status-only
// updates from their own controllers; those bump resourceVersion but not
// generation, and carry no information for resources connecting to the
// cluster — connection details are derived from the cluster spec.
var ClusterSourcePredicate predicate.Predicate = predicate.GenerationChangedPredicate{}

// ClusterSourceWatchOptions returns builder.WatchesOptions extending a watch
// with ClusterSourcePredicate. It is meant for watches on cluster CRs
// (Redpanda, V1 Cluster) whose event handlers fan out to every resource
// referencing the cluster: without the predicate each status-only update
// re-enqueues every referencing resource, keeping the workqueue saturated
// and starving interval-based requeues.
//
// Do NOT use these options for Secret or other non-generation-bearing
// watches: such objects never bump metadata.generation, so the predicate
// would suppress all of their update events.
func ClusterSourceWatchOptions() []builder.WatchesOption {
	return []builder.WatchesOption{builder.WithPredicates(ClusterSourcePredicate)}
}
