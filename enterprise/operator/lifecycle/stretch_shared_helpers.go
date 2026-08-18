// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The helpers below are duped from the OSS v2 lifecycle implementation. They
// are shared (used by both the v2 and stretch paths) but live in v2_*.go in
// the OSS tree; rather than drag those chart-coupled v2 files over, we dupe
// the chart-free helpers the stretch managers depend on.

func isNodePool(object client.Object) bool {
	_, ok := object.(*appsv1.StatefulSet)
	return ok
}

func setAndDirtyCheck[T comparable](source *T, value T) bool {
	if *source != value {
		*source = value
		return true
	}
	return false
}
