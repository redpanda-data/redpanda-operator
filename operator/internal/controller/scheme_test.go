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
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// telemetryCollectedLists are the types the telemetry collector lists
// (internal/telemetry/collector.go). Add any new one here.
var telemetryCollectedLists = []client.ObjectList{
	&redpandav1alpha2.RedpandaList{},
	&redpandav1alpha2.NodePoolList{},
	&redpandav1alpha2.StretchClusterList{},
	&redpandav1alpha2.RedpandaBrokerPoolList{},
	&redpandav1alpha2.ConsoleList{},
	&redpandav1alpha2.PipelineList{},
	&vectorizedv1alpha1.ClusterList{},
}

// TestUnifiedSchemeRecognizesTelemetryTypes pins that the `run` command's scheme
// covers everything the telemetry collector lists.
//
// A type absent from the scheme is not a missing CRD or missing RBAC — it is a
// client-side serialization failure ("no kind is registered for the type
// v1alpha1.ClusterList in scheme"), and until the collector learned to tolerate
// it, one such type made the operator drop every telemetry report for the life
// of the process.
func TestUnifiedSchemeRecognizesTelemetryTypes(t *testing.T) {
	for _, list := range telemetryCollectedLists {
		_, _, err := UnifiedScheme.ObjectKinds(list)
		require.NoErrorf(t, err, "unified scheme does not recognize %T", list)
	}
}

// TestMulticlusterSchemeOmitsVectorizedByDesign records a deliberate asymmetry
// rather than an oversight.
//
// The multicluster command does not reconcile the legacy vectorized Cluster API,
// so that type stays out of its scheme: the schemes are meant to match the CRDs
// each command actually installs, so nobody has to work out what is installed
// versus what is merely registered with the client.
//
// The collector lists that type anyway, and gets a NotRegistered error here.
// That is safe only because Collector.list tolerates NotRegistered alongside
// NoMatch and Forbidden — see TestCollect_ToleratesTypeMissingFromScheme in
// internal/telemetry, which is the test that makes this omission survivable.
// The visible consequence is that legacy vectorized Clusters running alongside a
// multicluster install are not counted in telemetry; they are not managed by
// that command either.
//
// If this assertion ever fails because the type was registered, delete the test
// — but check first that it was registered for a reason beyond telemetry.
func TestMulticlusterSchemeOmitsVectorizedByDesign(t *testing.T) {
	_, _, err := MulticlusterScheme.ObjectKinds(&vectorizedv1alpha1.ClusterList{})
	require.Error(t, err, "multicluster scheme unexpectedly recognizes the legacy vectorized Cluster API")

	// Everything else the collector lists must still be recognized: the
	// multicluster command does reconcile those, and a NotRegistered error there
	// would be a real bug rather than a documented gap.
	for _, list := range telemetryCollectedLists {
		if _, ok := list.(*vectorizedv1alpha1.ClusterList); ok {
			continue
		}
		_, _, err := MulticlusterScheme.ObjectKinds(list)
		require.NoErrorf(t, err, "multicluster scheme does not recognize %T", list)
	}
}
