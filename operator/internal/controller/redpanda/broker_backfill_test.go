// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestBackfillRotationKeysCoversAllRotationKeys pins the adoption half of the
// no-spurious-roll invariant: backfillRotationKeys exists so that an adopted
// pod — whose config the migration preconditions verified is current — is
// never queued for a pointless rotation. That only holds if the backfill
// stamps EVERY rotation-identity key the template carries: any key it skips
// leaves the adopted pod PodOutdated the moment the template holds a value
// for it, and with restart-cluster-on-config-change enabled the cluster-config
// version key is stamped into every template by MarkForRestart — so a skipped
// key means a serialized full-fleet restart right after a migration completes.
func TestBackfillRotationKeysCoversAllRotationKeys(t *testing.T) {
	broker := &redpandav1alpha2.Broker{
		Spec: redpandav1alpha2.BrokerSpec{
			PodTemplate: redpandav1alpha2.BrokerPodTemplate{
				Annotations: map[string]string{
					redpandav1alpha2.BrokerPodTemplateHashAnnotation:      "hash-1",
					redpandav1alpha2.BrokerConfigChecksumAnnotation:       "checksum-1",
					redpandav1alpha2.BrokerClusterConfigVersionAnnotation: "config-v1",
				},
			},
		},
	}
	pod := &corev1.Pod{}

	require.True(t, backfillRotationKeys(broker, pod), "an annotation-less pod must get stamped")

	for _, key := range redpandav1alpha2.RotationAnnotations {
		assert.Equalf(t, broker.Spec.PodTemplate.Annotations[key], pod.Annotations[key],
			"rotation key %q was not backfilled onto the adopted pod", key)
	}
	assert.False(t, broker.PodOutdated(pod),
		"an adopted pod must not be pending a rotation immediately after backfill")
}
