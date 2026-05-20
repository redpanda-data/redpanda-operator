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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// nodePortService returns NodePort Services for external access, one per
// local NodePool. External config (enabled, type, advertised ports) is
// per-pool, so each pool gets its own NodePort Service tied to its own pods.
//
// NodePorts are a cluster-wide allocation, so two local NodePools that both
// default to the same advertised ports cannot both have their Service
// created — the Kubernetes API server rejects the second one. To avoid the
// resulting reconcile-error loop, we resolve collisions deterministically
// here: the lexically-first pool wins each nodePort, and any pool that
// would clash with an already-claimed port has its Service skipped and is
// recorded in state.externalNodePortConflicts. The NodePoolReconciler
// independently runs the same detection via DetectExternalNodePortConflicts
// and surfaces ExternalAccessReady=False/NodePortConflict on each affected
// pool so users see why their external Service is missing.
func nodePortService(state *RenderState) []*corev1.Service {
	var out []*corev1.Service
	// portOwner tracks which pool first claimed each nodePort so we can
	// attribute conflicts back to a specific pool name in the condition.
	// state.inClusterPools is already sorted by NewRenderState, so the
	// lexically-first pool that requests a given port wins it.
	portOwner := map[int32]string{}
	for _, pool := range state.inClusterPools {
		svc, conflict := nodePortServiceForPool(state, pool, portOwner)
		if conflict != nil {
			state.externalNodePortConflicts = append(state.externalNodePortConflicts, *conflict)
			continue
		}
		if svc != nil {
			out = append(out, svc)
		}
	}
	return out
}

// DetectExternalNodePortConflicts runs the same collision detection that
// the renderer applies in nodePortService, but without producing Service
// objects. It is intended for the NodePoolReconciler to populate the
// ExternalAccessReady condition on each pool without having to call the
// full renderer.
//
// localPools are the NodePools that live in the same Kubernetes cluster
// and share the cluster-wide nodePort allocation; cluster is the parent
// StretchCluster whose defaults the pools inherit. Both inputs are
// deep-copied internally, so the caller's objects are not mutated.
//
// The returned slice is keyed by losing pool name: an entry is present iff
// that pool would clash with the lexically-first pool that claimed the
// same nodePort. Pools that have external disabled, set
// `external.enabled=false`, opt out of the NodePort Service via
// `external.service.enabled=false`, or use a non-NodePort external type
// have no entry — those are reported as Disabled / Available by the
// caller.
func DetectExternalNodePortConflicts(cluster *redpandav1alpha2.StretchCluster, localPools []*redpandav1alpha2.NodePool) []ExternalNodePortConflict {
	if len(localPools) <= 1 {
		return nil
	}

	// Work on copies — MergeDefaultsFrom mutates pool specs in place.
	// When the parent StretchCluster is briefly missing (a race between
	// pool creation and the parent's cache landing), use a zero-value
	// spec as defaulting source. Listener / external defaults live on the
	// pool itself and still kick in, so conflict detection produces the
	// same answer it would once the parent reappears.
	var defaultedClusterSpec redpandav1alpha2.StretchClusterSpec
	if cluster != nil {
		defaultedClusterSpec = *cluster.Spec.DeepCopy()
	}
	defaultedClusterSpec.MergeDefaults()
	pools := make([]*redpandav1alpha2.NodePool, len(localPools))
	for i, p := range localPools {
		pools[i] = p.DeepCopy()
		pools[i].Spec.MergeDefaultsFrom(&defaultedClusterSpec)
	}
	sort.Slice(pools, func(i, j int) bool {
		return pools[i].Name < pools[j].Name
	})

	portOwner := map[int32]string{}
	var conflicts []ExternalNodePortConflict
	for _, pool := range pools {
		ext := pool.Spec.External
		if ext == nil || !ext.IsEnabled() {
			continue
		}
		if ext.Service != nil && !ext.Service.IsEnabled() {
			continue
		}
		if ext.GetType() != string(corev1.ServiceTypeNodePort) {
			continue
		}

		ports := externalServicePorts(pool.Spec.Listeners, true)
		var conflicting []int32
		var conflictsWith string
		for _, p := range ports {
			if p.NodePort == 0 {
				continue
			}
			if owner, taken := portOwner[p.NodePort]; taken {
				conflicting = append(conflicting, p.NodePort)
				if conflictsWith == "" {
					conflictsWith = owner
				}
			}
		}
		if len(conflicting) > 0 {
			conflicts = append(conflicts, ExternalNodePortConflict{
				Pool:          pool.Name,
				ConflictsWith: conflictsWith,
				Ports:         conflicting,
			})
			continue
		}
		for _, p := range ports {
			if p.NodePort == 0 {
				continue
			}
			portOwner[p.NodePort] = pool.Name
		}
	}
	return conflicts
}

// nodePortServiceForPool renders one NodePort Service for the given pool.
//
// portOwner is a running map of (nodePort -> owning pool name) across all
// previously-rendered pools in this local K8s cluster; if any of this
// pool's requested nodePorts is already in the map, the pool's Service is
// skipped and an ExternalNodePortConflict describing the collision is
// returned instead. When the pool wins all of its requested nodePorts,
// portOwner is updated to reflect the new claims.
func nodePortServiceForPool(state *RenderState, pool *redpandav1alpha2.NodePool, portOwner map[int32]string) (*corev1.Service, *ExternalNodePortConflict) {
	ext := state.PoolSpec(pool).External
	if ext == nil || !ext.IsEnabled() {
		return nil, nil
	}
	if ext.Service != nil && !ext.Service.IsEnabled() {
		return nil, nil
	}
	if ext.GetType() != string(corev1.ServiceTypeNodePort) {
		return nil, nil
	}

	ports := externalServicePorts(state.PoolSpec(pool).Listeners, true)
	if len(ports) == 0 {
		return nil, nil
	}

	// Detect collisions before claiming any port — we want a Conflict
	// outcome to be all-or-nothing per pool rather than partially-rendered.
	var conflictingPorts []int32
	var conflictsWith string
	for _, p := range ports {
		if p.NodePort == 0 {
			continue
		}
		if owner, taken := portOwner[p.NodePort]; taken {
			conflictingPorts = append(conflictingPorts, p.NodePort)
			if conflictsWith == "" {
				conflictsWith = owner
			}
		}
	}
	if len(conflictingPorts) > 0 {
		return nil, &ExternalNodePortConflict{
			Pool:          pool.Name,
			ConflictsWith: conflictsWith,
			Ports:         conflictingPorts,
		}
	}

	// No collisions — claim the ports for this pool.
	for _, p := range ports {
		if p.NodePort == 0 {
			continue
		}
		portOwner[p.NodePort] = pool.Name
	}

	annotations := ext.Annotations
	if annotations == nil {
		annotations = map[string]string{}
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-external", state.poolFullname(pool)),
			Namespace:   state.namespace,
			Labels:      state.commonLabels(),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
			Ports:                    ports,
			PublishNotReadyAddresses: true,
			Selector:                 nodePortSelector(state, pool),
			SessionAffinity:          corev1.ServiceAffinityNone,
			Type:                     corev1.ServiceTypeNodePort,
		},
	}, nil
}

func nodePortSelector(state *RenderState, pool *redpandav1alpha2.NodePool) map[string]string {
	sel := state.clusterPodLabelsSelector()
	sel[labelComponentKey] = "redpanda" + pool.Suffix()
	return sel
}
