// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestRedpandaSchemaRegistryEnabled(t *testing.T) {
	t.Run("defaults to enabled through every unset level", func(t *testing.T) {
		assert.True(t, redpandaSchemaRegistryEnabled(nil))
		assert.True(t, redpandaSchemaRegistryEnabled(&redpandav1alpha2.Redpanda{}))
		assert.True(t, redpandaSchemaRegistryEnabled(&redpandav1alpha2.Redpanda{
			Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{}},
		}))
		assert.True(t, redpandaSchemaRegistryEnabled(&redpandav1alpha2.Redpanda{
			Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Listeners: &redpandav1alpha2.Listeners{SchemaRegistry: &redpandav1alpha2.SchemaRegistry{}},
			}},
		}))
	})

	t.Run("explicit setting wins", func(t *testing.T) {
		mk := func(enabled bool) *redpandav1alpha2.Redpanda {
			return &redpandav1alpha2.Redpanda{
				Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
					Listeners: &redpandav1alpha2.Listeners{SchemaRegistry: &redpandav1alpha2.SchemaRegistry{
						Listener: redpandav1alpha2.Listener{Enabled: ptr.To(enabled)},
					}},
				}},
			}
		}
		assert.False(t, redpandaSchemaRegistryEnabled(mk(false)))
		assert.True(t, redpandaSchemaRegistryEnabled(mk(true)))
	})
}

// TestSchemaRegistryDisabledSentinel covers #1793: a v2 cluster with the
// Schema Registry listener disabled yields the not-configured sentinel (and
// a nil ACL client) instead of a failed SRV lookup that bricks User/Role/
// Group reconciliation. Runs on a zero Factory: the gate must trip before
// any config or DNS access.
func TestSchemaRegistryDisabledSentinel(t *testing.T) {
	cluster := &redpandav1alpha2.Redpanda{
		Spec: redpandav1alpha2.RedpandaSpec{ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
			Listeners: &redpandav1alpha2.Listeners{SchemaRegistry: &redpandav1alpha2.SchemaRegistry{
				Listener: redpandav1alpha2.Listener{Enabled: ptr.To(false)},
			}},
		}},
	}

	f := &Factory{}
	_, err := f.SchemaRegistryClientForCluster(t.Context(), cluster, mcmanager.LocalCluster)
	require.ErrorIs(t, err, NoSchemaRegistryAPI)
	// SchemaRegistryACLClientForCluster maps this to (nil, nil).
	require.True(t, isSchemaRegistryNotConfigured(err))
}
