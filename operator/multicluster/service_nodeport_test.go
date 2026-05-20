// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// stretchClusterWithDefaultExternalNodePort returns a StretchCluster
// whose listeners all enable external NodePort access with the default
// advertised-port set. Pools that inherit these defaults will all request
// the same node-wide ports.
func stretchClusterWithDefaultExternalNodePort(name string) *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cluster.redpanda.com/v1alpha2",
			Kind:       "StretchCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
}

func nodePoolWithDefaultExternal(name, clusterName string) *redpandav1alpha2.NodePool {
	return &redpandav1alpha2.NodePool{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "cluster.redpanda.com/v1alpha2",
			Kind:       "NodePool",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: redpandav1alpha2.NodePoolSpec{
			EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
				Replicas: ptr.To(int32(1)),
				External: &redpandav1alpha2.External{
					Enabled: ptr.To(true),
					Type:    ptr.To("NodePort"),
				},
			},
			ClusterRef: redpandav1alpha2.ClusterRef{
				Name: clusterName,
				Kind: ptr.To(redpandav1alpha2.StretchClusterRefKind),
			},
		},
	}
}

// DetectExternalNodePortConflicts is called from the NodePoolReconciler with
// a nil StretchCluster when the parent is briefly missing (cache race
// between pool create and parent visibility). The function must still
// produce a meaningful answer so the condition transitions off the default
// "NotReconciled" reason — otherwise the reconciler writes a stale Unknown
// condition forever.
func TestDetectExternalNodePortConflicts_NilClusterStillDetectsConflict(t *testing.T) {
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("beta", "c"),
		nodePoolWithDefaultExternal("alpha", "c"),
	}

	conflicts := DetectExternalNodePortConflicts(nil, pools)
	require.Len(t, conflicts, 1, "conflict detection must work even when the parent StretchCluster is briefly missing")
	require.Equal(t, "beta", conflicts[0].Pool)
	require.Equal(t, "alpha", conflicts[0].ConflictsWith)
}

func TestDetectExternalNodePortConflicts_NoConflictForSinglePool(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{nodePoolWithDefaultExternal("solo", "c")}

	conflicts := DetectExternalNodePortConflicts(sc, pools)
	require.Empty(t, conflicts, "single pool cannot conflict with itself")
}

func TestDetectExternalNodePortConflicts_TwoPoolsDefaultExternal(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	// Lexically: "alpha" < "beta", so alpha wins each port and beta is reported.
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("beta", "c"),
		nodePoolWithDefaultExternal("alpha", "c"),
	}

	conflicts := DetectExternalNodePortConflicts(sc, pools)
	require.Len(t, conflicts, 1, "exactly one pool should be reported as conflicting")
	require.Equal(t, "beta", conflicts[0].Pool)
	require.Equal(t, "alpha", conflicts[0].ConflictsWith)
	require.NotEmpty(t, conflicts[0].Ports, "conflict must list at least one colliding port")
}

func TestDetectExternalNodePortConflicts_DistinctAdvertisedPorts(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("alpha", "c"),
		nodePoolWithDefaultExternal("beta", "c"),
	}
	// Push beta's external listeners onto a distinct advertised-port range
	// so no nodePort overlaps with alpha's defaults.
	pools[1].Spec.Listeners = &redpandav1alpha2.StretchListeners{
		Admin: &redpandav1alpha2.StretchAPIListener{
			External: map[string]*redpandav1alpha2.StretchExternalListener{
				"default": {
					StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(9645))},
					AdvertisedPorts: []int32{40644},
				},
			},
		},
		Kafka: &redpandav1alpha2.StretchAPIListener{
			External: map[string]*redpandav1alpha2.StretchExternalListener{
				"default": {
					StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(9094))},
					AdvertisedPorts: []int32{40092},
				},
			},
		},
		HTTP: &redpandav1alpha2.StretchAPIListener{
			External: map[string]*redpandav1alpha2.StretchExternalListener{
				"default": {
					StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(8084))},
					AdvertisedPorts: []int32{40082},
				},
			},
		},
		SchemaRegistry: &redpandav1alpha2.StretchAPIListener{
			External: map[string]*redpandav1alpha2.StretchExternalListener{
				"default": {
					StretchListener: redpandav1alpha2.StretchListener{Port: ptr.To(int32(8085))},
					AdvertisedPorts: []int32{40081},
				},
			},
		},
	}

	conflicts := DetectExternalNodePortConflicts(sc, pools)
	require.Empty(t, conflicts, "distinct advertisedPorts should produce no conflict")
}

func TestDetectExternalNodePortConflicts_DoesNotMutateInputs(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("alpha", "c"),
		nodePoolWithDefaultExternal("beta", "c"),
	}
	scBefore := sc.DeepCopy()
	poolsBefore := []*redpandav1alpha2.NodePool{pools[0].DeepCopy(), pools[1].DeepCopy()}

	_ = DetectExternalNodePortConflicts(sc, pools)

	require.Equal(t, scBefore, sc, "input StretchCluster must not be mutated by detection")
	require.Equal(t, poolsBefore[0], pools[0], "input pool[0] must not be mutated")
	require.Equal(t, poolsBefore[1], pools[1], "input pool[1] must not be mutated")
}

func TestNodePortService_SkipsConflictingPools(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("beta", "c"),
		nodePoolWithDefaultExternal("alpha", "c"),
	}

	state, err := NewRenderState(nil, sc, pools, pools, "test")
	require.NoError(t, err)

	services := nodePortService(state)
	require.Len(t, services, 1, "only the lexically-first pool should produce a Service")
	require.Contains(t, services[0].Name, "alpha", "alpha wins; produced Service name should reflect that")

	conflicts := state.ExternalNodePortConflicts()
	require.Len(t, conflicts, 1)
	require.Equal(t, "beta", conflicts[0].Pool)
	require.Equal(t, "alpha", conflicts[0].ConflictsWith)
}

func TestNodePortService_ExternalDisabled_NoConflictRecorded(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("alpha", "c"),
		nodePoolWithDefaultExternal("beta", "c"),
	}
	// Disable external on beta — it should be skipped silently, not
	// recorded as a conflict.
	pools[1].Spec.External.Enabled = ptr.To(false)

	state, err := NewRenderState(nil, sc, pools, pools, "test")
	require.NoError(t, err)

	services := nodePortService(state)
	require.Len(t, services, 1, "only alpha has external; beta is disabled")
	require.Empty(t, state.ExternalNodePortConflicts(), "external-disabled pool is not a conflict")
}

// TestNodePortService_PortOwnerAttributesByName guards against a regression
// where the conflict's ConflictsWith would be empty (no port owner
// recorded) — that would break the surfaced status message.
func TestNodePortService_PortOwnerAttributesByName(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("zeta", "c"),
		nodePoolWithDefaultExternal("alpha", "c"),
	}

	state, err := NewRenderState(nil, sc, pools, pools, "test")
	require.NoError(t, err)

	_ = nodePortService(state)
	conflicts := state.ExternalNodePortConflicts()
	require.Len(t, conflicts, 1)
	require.NotEmpty(t, conflicts[0].ConflictsWith, "ConflictsWith must name the pool that won the port")
	require.Equal(t, "alpha", conflicts[0].ConflictsWith)
}

// TestNodePortService_NonNodePortExternal_NotTrackedForConflicts confirms
// that LoadBalancer-type external services don't reserve nodePorts in our
// bookkeeping. Two LB-typed pools coexist cleanly.
func TestNodePortService_NonNodePortExternal_NotTrackedForConflicts(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{
		nodePoolWithDefaultExternal("alpha", "c"),
		nodePoolWithDefaultExternal("beta", "c"),
	}
	pools[0].Spec.External.Type = ptr.To("LoadBalancer")
	pools[1].Spec.External.Type = ptr.To("LoadBalancer")

	state, err := NewRenderState(nil, sc, pools, pools, "test")
	require.NoError(t, err)

	services := nodePortService(state)
	require.Empty(t, services, "LoadBalancer pools must not produce NodePort Services")
	require.Empty(t, state.ExternalNodePortConflicts(), "LoadBalancer external is not a NodePort-conflict source")
}

// Trivial sanity check that the conflicting nodePort exists in the
// generated Service's port list for the winning pool — protects the
// renderer from accidentally clearing NodePort=0 for the winner.
func TestNodePortService_WinnerHasNonZeroNodePorts(t *testing.T) {
	sc := stretchClusterWithDefaultExternalNodePort("c")
	pools := []*redpandav1alpha2.NodePool{nodePoolWithDefaultExternal("solo", "c")}

	state, err := NewRenderState(nil, sc, pools, pools, "test")
	require.NoError(t, err)

	services := nodePortService(state)
	require.Len(t, services, 1)
	var any int32
	for _, p := range services[0].Spec.Ports {
		if p.NodePort != 0 {
			any = p.NodePort
			break
		}
	}
	require.NotZero(t, any, "winning pool's Service should have at least one explicit NodePort")
	require.Equal(t, corev1.ServiceTypeNodePort, services[0].Spec.Type)
	require.True(t, strings.HasSuffix(services[0].Name, "-external"))
}
