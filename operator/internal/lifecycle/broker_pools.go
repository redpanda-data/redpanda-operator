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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

// fetchBrokerBackedPools returns synthesized poolWithOrdinals for node pools
// whose brokers are managed by Broker CRs rather than a StatefulSet. It lists
// the owner's Broker CRs, groups them by node pool, resolves each Broker's pod
// by its deterministic name, and wraps the result in an STS-shaped facade so
// the PoolTracker's status/readiness/scale accounting keeps working after the
// StatefulSet is gone.
//
// Pools named in skip (pools that still have a live StatefulSet — i.e. broker
// migration has not completed for them) are left out: while the STS exists it
// remains the authoritative view and any Broker CRs for it are inert shadows.
//
// The facade's fields map to broker-mode semantics as follows:
//   - Spec.Replicas: Broker CRs without decommission intent (the pool's
//     current scale intent)
//   - Status.Replicas: broker pods that exist
//   - Status.ReadyReplicas / AvailableReplicas: broker pods passing readiness
//   - Status.UpdatedReplicas: broker pods whose rotation identity matches
//     their Broker's desired pod template (see Broker.PodOutdated)
//
// CheckScale's "spec vs status vs pod count" comparison therefore reads
// naturally: a decommission in flight or a pod mid-rotation reports an active
// scale operation, and PoolStatuses' CondemnedReplicas surfaces pods of
// decommissioning brokers.
func (r *ResourceClient[T, U]) fetchBrokerBackedPools(ctx context.Context, ctl *kube.Ctl, owner U, clusterName, canonical string, skip map[string]bool) ([]*poolWithOrdinals, error) {
	logger := log.FromContext(ctx).WithName("fetchBrokerBackedPools")

	ownerLabels := r.ownershipResolver.GetOwnerLabels(owner)
	listCtx, listCancel := context.WithTimeout(ctx, CallTimeoutFor(clusterName))
	brokers, err := kube.List[redpandav1alpha2.BrokerList](listCtx, ctl, owner.GetNamespace(), client.MatchingLabels(ownerLabels))
	listCancel()
	if err != nil {
		return nil, fmt.Errorf("listing Broker CRs: %w", err)
	}

	// Group controller-owned Brokers by pod-name base (the would-be
	// StatefulSet name), which is also how existing/desired pools correlate
	// in the tracker.
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
			// A live StatefulSet still owns this pool (mid-migration shadow
			// Brokers) — the STS view is authoritative.
			continue
		}

		var pods []*corev1.Pod
		var specReplicas, ready, upToDate int32
		for _, b := range group {
			if b.IsDiskLost() {
				// A dead incarnation is not a live pool member — and after
				// index release its pod NAME belongs to the replacement, so
				// counting it would double-count the pair. It still anchors
				// the group above: a drained pool whose last broker is a
				// tombstone must keep its facade so the engine's tombstone
				// lifecycle can run for it.
				continue
			}
			// A decommissioning broker leaves the scale intent but its pod
			// keeps counting below — spec < status is how CheckScale reports
			// the scale-down as in flight, and CondemnedReplicas surfaces the
			// pod. Skipping the whole broker here would make an in-flight
			// decommission look converged.
			if !b.Spec.Decommission {
				specReplicas++
			}

			getCtx, getCancel := context.WithTimeout(ctx, CallTimeoutFor(clusterName))
			pod, err := kube.Get[corev1.Pod](getCtx, ctl, kube.ObjectKey{Namespace: b.Namespace, Name: b.PodName()})
			getCancel()
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("getting pod for Broker %s: %w", b.Name, err)
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

		// The facade carries the pool identity of its Brokers: consumers
		// (brokerSetFor's drain path for pools removed from the spec) must be
		// able to recover the pool and cluster names without a desired
		// render to correlate against.
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
			set:  newMulticlusterStatefulSet(facade, clusterName, canonical),
			pods: withOrdinals,
			// No ControllerRevisions: broker-mode rotation identity lives in
			// pod annotations (Broker.PodOutdated), not STS revisions. The
			// brokerBacked flag excludes these pools from the STS mutation
			// planners and the revision-based roll helpers.
			brokerBacked: true,
		})
	}

	return pools, nil
}

// brokerPodNameBase derives the pod-name prefix shared by a Broker's pool —
// the name the pool's StatefulSet had (or would have had). It strips the
// network-index suffix from the Broker's deterministic pod name. It returns
// "" for a Broker without a network index (no pod name is derivable);
// callers must skip such Brokers rather than group them.
func brokerPodNameBase(b *redpandav1alpha2.Broker) string {
	if b.Spec.NetworkIndex == nil {
		return ""
	}
	return strings.TrimSuffix(b.PodName(), fmt.Sprintf("-%d", *b.Spec.NetworkIndex))
}
