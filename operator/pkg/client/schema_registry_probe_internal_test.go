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
	"k8s.io/utils/ptr"

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

func TestSchemaRegistryProbeUniform(t *testing.T) {
	mkPool := func(mutate func(*redpandav1alpha2.StretchAPIListener)) *redpandav1alpha2.RedpandaBrokerPool {
		pool := &redpandav1alpha2.RedpandaBrokerPool{}
		if mutate != nil {
			listener := &redpandav1alpha2.StretchAPIListener{}
			mutate(listener)
			pool.Spec.Listeners = &redpandav1alpha2.StretchListeners{SchemaRegistry: listener}
		}
		return pool
	}

	t.Run("defaults to uniform", func(t *testing.T) {
		// No pools, nil pools, unset listener config, and explicit
		// enablement are all uniform: MergeDefaults renders an enabled
		// listener on the default port for every one of these.
		for _, pools := range [][]*redpandav1alpha2.RedpandaBrokerPool{
			nil,
			{nil, mkPool(nil)},
			{mkPool(nil), mkPool(func(l *redpandav1alpha2.StretchAPIListener) { l.Enabled = ptr.To(true) })},
		} {
			uniform, reason := SchemaRegistryProbeUniform(pools)
			assert.True(t, uniform, reason)
		}
	})

	t.Run("explicit disable is not probeable", func(t *testing.T) {
		// Per-broker probing against a pool that doesn't serve SR would
		// read as permanently unreachable.
		uniform, reason := SchemaRegistryProbeUniform([]*redpandav1alpha2.RedpandaBrokerPool{
			mkPool(func(l *redpandav1alpha2.StretchAPIListener) { l.Enabled = ptr.To(true) }),
			mkPool(func(l *redpandav1alpha2.StretchAPIListener) { l.Enabled = ptr.To(false) }),
		})
		assert.False(t, uniform)
		assert.Contains(t, reason, "explicitly disabled")
	})

	t.Run("heterogeneous ports are not probeable", func(t *testing.T) {
		// The client stamps the representative pool's port onto every
		// broker's endpoint, so a pool on a different port would probe as
		// connection-refused forever.
		uniform, reason := SchemaRegistryProbeUniform([]*redpandav1alpha2.RedpandaBrokerPool{
			mkPool(nil),
			mkPool(func(l *redpandav1alpha2.StretchAPIListener) { l.Port = ptr.To(int32(9081)) }),
		})
		assert.False(t, uniform)
		assert.Contains(t, reason, "differs from its peers")
	})

	t.Run("heterogeneous TLS is not probeable", func(t *testing.T) {
		uniform, reason := SchemaRegistryProbeUniform([]*redpandav1alpha2.RedpandaBrokerPool{
			mkPool(nil),
			mkPool(func(l *redpandav1alpha2.StretchAPIListener) {
				l.TLS = &redpandav1alpha2.StretchListenerTLS{Enabled: ptr.To(true)}
			}),
		})
		assert.False(t, uniform)
		assert.Contains(t, reason, "differs from its peers")
	})

	t.Run("homogeneous non-default config is probeable", func(t *testing.T) {
		custom := func(l *redpandav1alpha2.StretchAPIListener) { l.Port = ptr.To(int32(9081)) }
		uniform, reason := SchemaRegistryProbeUniform([]*redpandav1alpha2.RedpandaBrokerPool{
			mkPool(custom), mkPool(custom),
		})
		assert.True(t, uniform, reason)
	})
}
