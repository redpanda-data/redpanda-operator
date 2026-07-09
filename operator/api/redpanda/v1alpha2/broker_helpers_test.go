// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestBrokerPodName(t *testing.T) {
	v1Broker := func(pool string, index int32) *Broker {
		b := &Broker{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns"},
			Spec: BrokerSpec{
				ClusterRef: ClusterRef{
					Group: ptr.To("redpanda.vectorized.io"),
					Kind:  ptr.To("Cluster"),
					Name:  "rp",
				},
				NetworkIndex: ptr.To(index),
			},
		}
		if pool != "" {
			b.Labels = map[string]string{NodePoolLabel: pool}
		}
		return b
	}

	// Default pool (explicit or unlabeled): pods carry the bare cluster name.
	assert.Equal(t, "rp-0", v1Broker("default", 0).PodName())
	assert.Equal(t, "rp-2", v1Broker("", 2).PodName())
	// Pools declared in Cluster.spec.nodePools: <cluster>-<pool>-<ordinal>,
	// matching the per-pool StatefulSet naming.
	assert.Equal(t, "rp-high-mem-1", v1Broker("high-mem", 1).PodName())

	// NodePool ref: ClusterRef names the pool, the cluster half comes from
	// ClusterNameLabel — pods are <cluster>-<pool>-<ordinal>.
	nodePoolBroker := &Broker{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Labels:    map[string]string{ClusterNameLabel: "rp"},
		},
		Spec: BrokerSpec{
			ClusterRef: ClusterRef{
				Group: ptr.To("cluster.redpanda.com"),
				Kind:  ptr.To("NodePool"),
				Name:  "high-mem",
			},
			NetworkIndex: ptr.To(int32(1)),
		},
	}
	assert.Equal(t, "rp-high-mem-1", nodePoolBroker.PodName())
}
