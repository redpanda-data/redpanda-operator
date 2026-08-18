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
	"context"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	multiclusterRenderer "github.com/redpanda-data/redpanda-operator/operator/multicluster"
)

// This file holds the stretch-cluster-only pieces of the resource client so
// the generic framework files stay free of BrokerPool/renderer couplings.

// BrokerPoolGenerationLabel is the generation label applied to StatefulSets
// rendered from RedpandaBrokerPools (the stretch-cluster pool kind).
const BrokerPoolGenerationLabel = multiclusterRenderer.BrokerPoolLabelGeneration

// FetchExistingBrokerPoolsFromAllClusters returns the union of BrokerPools
// referencing the given cluster across every engaged cluster, plus the set of
// cluster names whose List actually succeeded (vs. being probe-skipped or
// failing the call). The observed set is load-bearing for downstream
// scale-down safety: when a cluster's BrokerPool list wasn't observed, the
// renderer downstream can produce a desiredCount=0 for that cluster purely
// because we never saw its BrokerPools — indistinguishable from a real
// deletion. Callers must gate any "no desired counterpart → drain"
// decision on the observed set so a transient fetch failure on a
// partitioned peer can't be misread as user intent to remove all pools.
func (r *ResourceClient[T, U]) FetchExistingBrokerPoolsFromAllClusters(ctx context.Context, cluster U) ([]*BrokerPoolInCluster, map[string]bool, error) {
	logger := log.FromContext(ctx)
	var nodePools []*BrokerPoolInCluster
	observed := map[string]bool{}
	for _, clusterName := range r.clusterList(cluster) {
		canonicalName := CanonicalClusterName(clusterName, r.manager.GetLocalClusterName)
		if clusterName != mcmanager.LocalCluster && !r.manager.IsClusterReachable(clusterName) {
			logger.Info("remote cluster unreachable (probe) in FetchExistingBrokerPoolsFromAllClusters, skipping", "cluster", canonicalName)
			continue
		}
		ctl, err := r.ctl(ctx, clusterName)
		if err != nil {
			if clusterName != mcmanager.LocalCluster {
				logger.Info("remote cluster unreachable in FetchExistingBrokerPoolsFromAllClusters, skipping", "cluster", canonicalName, "error", err)
				continue
			}
			return nil, nil, err
		}
		listCtx, listCancel := context.WithTimeout(ctx, CallTimeoutFor(clusterName))
		allNodePools, err := kube.List[redpandav1alpha2.RedpandaBrokerPoolList](listCtx, ctl, cluster.GetNamespace())
		listCancel()
		if err != nil {
			if clusterName != mcmanager.LocalCluster {
				logger.Info("could not list BrokerPools on remote cluster, skipping", "cluster", canonicalName, "error", err)
				continue
			}
			return nil, nil, err
		}
		observed[clusterName] = true
		for _, pool := range allNodePools.Items {
			clusterRef := pool.Spec.ClusterRef
			if clusterRef.IsStretchCluster() && clusterRef.Name == cluster.GetName() {
				nodePools = append(nodePools, &BrokerPoolInCluster{
					cluster:    canonicalName,
					brokerPool: pool.DeepCopy(),
				})
			}
		}
	}
	return nodePools, observed, nil
}
