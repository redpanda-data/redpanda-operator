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
	"github.com/redpanda-data/common-go/kube"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// RenderClusterResources returns the resources whose name and content are
// scoped to the StretchCluster as a whole — one set per K8s cluster,
// independent of how many local BrokerPools exist. Naming convention:
// <cluster>[-<purpose>].
//
// Membership:
//   - podDisruptionBudget — pure cluster-wide
//   - serviceInternal (headless ClusterIP) — must span all pools to act as
//     the cluster DNS root; port set derived from the first local pool
//     (representative-pool model)
//   - certIssuers — one Issuer per cert name, references the shared synced
//     CA Secret; cert-name set is the union across local pools' TLS configs
//   - clusterSecrets — SASL users + bootstrap-user (Auth is cluster-wide)
func RenderClusterResources(state *RenderState) ([]kube.Object, error) {
	clusterSecs, err := clusterSecrets(state)
	if err != nil {
		return nil, err
	}

	var manifests []kube.Object
	manifests = appendIfNotNil(manifests, podDisruptionBudget(state))
	manifests = appendIfNotNil(manifests, serviceInternal(state))
	manifests = appendIfNotNil(manifests, certIssuers(state)...)
	manifests = appendIfNotNil(manifests, clusterSecs...)
	return manifests, nil
}

// RenderInClusterPoolResources returns the resources scoped to a single
// local BrokerPool. Caller iterates state.InClusterPools() and aggregates.
// Naming convention: <cluster>-<pool>[-<purpose>].
//
// Membership:
//   - redpandaConfigMap — per-pool bootstrap + redpanda config
//   - rpkProfileConfigMapForPool — per-pool rpk profile (when External enabled)
//   - poolSecrets — per-pool lifecycle/configurator/fs-validator
//   - loadBalancerServicesForPool — per-pod LB Services within this pool
//   - serviceAccountForPool — per-pool SA
//   - serviceMonitorForPool — per-pool ServiceMonitor
//   - nodePortServiceForPool — per-pool external NodePort
//   - certificatesForPool — per-pool leaf certs (different DNS SANs per pool)
//   - rolesForPool / roleBindingsForPool — per-pool RBAC
//   - clusterRolesForPool / clusterRoleBindingsForPool — per-pool RBAC
//     (cluster-scoped objects but per-pool driven)
func RenderInClusterPoolResources(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ([]kube.Object, error) {
	cm, err := redpandaConfigMap(state, pool)
	if err != nil {
		return nil, err
	}

	lbs, err := loadBalancerServicesForPool(state, pool)
	if err != nil {
		return nil, err
	}

	certs, err := certificatesForPool(state, pool)
	if err != nil {
		return nil, err
	}

	var manifests []kube.Object
	manifests = appendIfNotNil(manifests, cm)
	manifests = appendIfNotNil(manifests, rpkProfileConfigMapForPool(state, pool))
	manifests = appendIfNotNil(manifests, poolSecrets(state, pool)...)
	manifests = appendIfNotNil(manifests, lbs...)
	manifests = appendIfNotNil(manifests, serviceAccountForPool(state, pool))
	manifests = appendIfNotNil(manifests, serviceMonitorForPool(state, pool))
	manifests = appendIfNotNil(manifests, nodePortServiceForPool(state, pool))
	manifests = appendIfNotNil(manifests, certs...)
	manifests = appendIfNotNil(manifests, rolesForPool(state, pool)...)
	manifests = appendIfNotNil(manifests, clusterRolesForPool(state, pool)...)
	manifests = appendIfNotNil(manifests, roleBindingsForPool(state, pool)...)
	manifests = appendIfNotNil(manifests, clusterRoleBindingsForPool(state, pool)...)
	return manifests, nil
}

// RenderEachPoolResources returns the resources emitted for one BrokerPool
// across the full cross-region pool set (local + remote). Caller iterates
// state.Pools() and aggregates. Each region's operator emits these for
// every broker so cross-cluster DNS / MCS / EndpointSlice resolution works.
// Naming convention: <cluster>-<pool>-<ordinal>.
func RenderEachPoolResources(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ([]kube.Object, error) {
	pps, err := perPodServicesForPool(state, pool)
	if err != nil {
		return nil, err
	}

	var manifests []kube.Object
	manifests = appendIfNotNil(manifests, pps...)
	manifests = appendIfNotNil(manifests, serviceExportsForPool(state, pool)...)
	manifests = appendIfNotNil(manifests, serviceImportsForPool(state, pool)...)
	manifests = appendIfNotNil(manifests, perPodEndpointsForPool(state, pool)...)
	return manifests, nil
}
