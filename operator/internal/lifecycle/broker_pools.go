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
	"fmt"
	"strings"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// fetchBrokerBackedPools synthesizes poolWithOrdinals for brokers managed
// by Broker CRs instead of a StatefulSet, wrapping them in an STS-shaped
// facade so the PoolTracker's status/readiness/scale accounting keeps
// working. Pools in skip still have a live, authoritative StatefulSet and
// are left out.
func (r *ResourceClient[T, U]) fetchBrokerBackedPools(ctx context.Context, ctl *kube.Ctl, owner U, clusterName, canonical string, skip map[string]bool) ([]*poolWithOrdinals, error) {
	logger := log.FromContext(ctx).WithName("fetchBrokerBackedPools")

	ownerLabels := r.ownershipResolver.GetOwnerLabels(owner)
	listCtx, listCancel := context.WithTimeout(ctx, CallTimeoutFor(clusterName))
	brokers, err := kube.List[redpandav1alpha2.BrokerList](listCtx, ctl, owner.GetNamespace(), client.MatchingLabels(ownerLabels))
	listCancel()
	if err != nil {
		return nil, fmt.Errorf("listing Broker CRs: %w", err)
	}

	// One labeled list instead of a GET per Broker: broker pods carry the
	// owner labels (stamped via the pod template), and per-broker round
	// trips would scale the reconcile's API traffic with the fleet size.
	podListCtx, podListCancel := context.WithTimeout(ctx, CallTimeoutFor(clusterName))
	podList, err := kube.List[corev1.PodList](podListCtx, ctl, owner.GetNamespace(), client.MatchingLabels(ownerLabels))
	podListCancel()
	if err != nil {
		return nil, fmt.Errorf("listing broker pods: %w", err)
	}
	podsByName := make(map[string]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		podsByName[podList.Items[i].Name] = &podList.Items[i]
	}

	// Group by pod-name base — the would-be StatefulSet name.
	groups := map[string][]*redpandav1alpha2.Broker{}
	for i := range brokers.Items {
		b := &brokers.Items[i]
		if !metav1.IsControlledBy(b, owner) {
			continue
		}
		base := brokerPodNameBase(b)
		if base == "" {
			logger.Info("skipping Broker with underivable pod name", "broker", b.Name)
			continue
		}
		groups[base] = append(groups[base], b)
	}

	pools := []*poolWithOrdinals{}
	for base, group := range groups {
		if skip[base] {
			continue
		}

		var pods []*corev1.Pod
		var specReplicas, ready, upToDate int32
		for _, b := range group {
			if b.IsDiskLost() {
				// Not a live pool member; its pod name may already belong to
				// its replacement.
				continue
			}
			// Excluded from Spec.Replicas but its pod still counts below, so
			// spec < status reports the scale-down as in flight.
			if !b.Spec.Decommission {
				specReplicas++
			}

			pod := podsByName[b.PodName()]
			if pod == nil {
				continue
			}
			pods = append(pods, pod)
			if utils.IsPodReady(pod) {
				ready++
			}
			if !b.PodOutdated(pod) {
				upToDate++
			}
		}

		withOrdinals, err := sortPodsByOrdinal(pods...)
		if err != nil {
			return nil, fmt.Errorf("sorting Broker pods by ordinal: %w", err)
		}

		// Lets consumers recover pool/cluster identity without a desired
		// render to correlate against (e.g. a pool removed from the spec).
		facadeLabels := map[string]string{}
		for _, key := range []string{redpandav1alpha2.NodePoolLabel, redpandav1alpha2.ClusterNameLabel} {
			if v := group[0].Labels[key]; v != "" {
				facadeLabels[key] = v
			}
		}

		facade := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      base,
				Namespace: owner.GetNamespace(),
				Labels:    facadeLabels,
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(specReplicas),
			},
			Status: appsv1.StatefulSetStatus{
				Replicas:          int32(len(pods)),
				ReadyReplicas:     ready,
				AvailableReplicas: ready,
				UpdatedReplicas:   upToDate,
			},
		}

		logger.V(log.TraceLevel).Info(
			"synthesized broker-backed pool",
			"cluster", canonical,
			"pool", base,
			"brokers", len(group),
			"specReplicas", specReplicas,
			"pods", len(pods),
			"ready", ready,
		)

		pools = append(pools, &poolWithOrdinals{
			set:          newMulticlusterStatefulSet(facade, clusterName, canonical),
			pods:         withOrdinals,
			brokerBacked: true,
		})
	}

	return pools, nil
}

// brokerPodNameBase strips the network-index suffix from a Broker's pod
// name, returning "" when no network index is set.
func brokerPodNameBase(b *redpandav1alpha2.Broker) string {
	if b.Spec.NetworkIndex == nil {
		return ""
	}
	return strings.TrimSuffix(b.PodName(), fmt.Sprintf("-%d", *b.Spec.NetworkIndex))
}
