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
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// TestSchemesRecognizeTelemetryTypes guards the types the telemetry collector
// lists (internal/telemetry/collector.go) against the schemes the commands
// actually run with. The collector tolerates a missing CRD and missing RBAC, but
// a type absent from the scheme is a client-side serialization failure, and
// before this was fixed it made the multicluster operator drop every report for
// the life of the process ("no kind is registered for the type
// v1alpha1.ClusterList in scheme"). Add any new type the collector lists here.
func TestSchemesRecognizeTelemetryTypes(t *testing.T) {
	collected := []client.ObjectList{
		&redpandav1alpha2.RedpandaList{},
		&redpandav1alpha2.NodePoolList{},
		&redpandav1alpha2.StretchClusterList{},
		&redpandav1alpha2.RedpandaBrokerPoolList{},
		&redpandav1alpha2.ConsoleList{},
		&redpandav1alpha2.PipelineList{},
		&vectorizedv1alpha1.ClusterList{},
	}

	for name, scheme := range map[string]*runtime.Scheme{
		"multicluster": MulticlusterScheme,
		"unified":      UnifiedScheme,
	} {
		for _, list := range collected {
			_, _, err := scheme.ObjectKinds(list)
			require.NoErrorf(t, err, "%s scheme does not recognize %T", name, list)
		}
	}
}
