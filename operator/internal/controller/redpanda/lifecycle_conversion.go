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
	"context"

	corev1 "k8s.io/api/core/v1"

	entcontroller "github.com/redpanda-data/redpanda-operator/enterprise/operator/controller"
	entlifecycle "github.com/redpanda-data/redpanda-operator/enterprise/operator/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

// This file is the type seam between the OSS lifecycle package (the generic
// framework backing the v2 RedpandaReconciler) and its enterprise
// concretization (which backs the stretch MulticlusterReconciler). The shared
// remediation cores — entcontroller.ClearStuckMaintenanceMode and
// entcontroller.StaleDiskWipe — are typed on the enterprise lifecycle's
// MulticlusterPod, so the RedpandaReconciler's thin entry points convert
// their OSS-typed pods at the boundary using the helpers below.
// The two MulticlusterPod types are field-for-field identical; conversion is
// a pure re-wrap that preserves the pod pointer and both cluster names.

// toEnterprisePods re-wraps OSS lifecycle pods as enterprise lifecycle pods.
func toEnterprisePods(pods []*lifecycle.MulticlusterPod) []*entlifecycle.MulticlusterPod {
	out := make([]*entlifecycle.MulticlusterPod, 0, len(pods))
	for _, pod := range pods {
		out = append(out, entlifecycle.NewMulticlusterPod(pod.Pod, pod.GetCluster(), pod.GetCanonicalClusterName()))
	}
	return out
}

// fromEnterprisePod re-wraps one enterprise lifecycle pod as an OSS lifecycle
// pod, for handing back to the OSS ResourceClient.
func fromEnterprisePod(pod *entlifecycle.MulticlusterPod) *lifecycle.MulticlusterPod {
	return lifecycle.NewMulticlusterPod(pod.Pod, pod.GetCluster(), pod.GetCanonicalClusterName())
}

// ossPodDeleter adapts the OSS lifecycle ResourceClient to the
// enterprise-typed PodDeleter interface consumed by the stale-disk wipe core.
type ossPodDeleter struct {
	client *lifecycle.ResourceClient[lifecycle.ClusterWithPools, *lifecycle.ClusterWithPools]
}

var _ entcontroller.PodDeleter = ossPodDeleter{}

func (d ossPodDeleter) DeletePVCsForPod(ctx context.Context, pod *entlifecycle.MulticlusterPod) error {
	return d.client.DeletePVCsForPod(ctx, fromEnterprisePod(pod))
}

func (d ossPodDeleter) DeletePod(ctx context.Context, pod *entlifecycle.MulticlusterPod) error {
	return d.client.DeletePod(ctx, fromEnterprisePod(pod))
}

func (d ossPodDeleter) GetLivePod(ctx context.Context, pod *entlifecycle.MulticlusterPod) (*corev1.Pod, error) {
	return d.client.GetLivePod(ctx, fromEnterprisePod(pod))
}

// ossPodLogsReader adapts the OSS lifecycle ResourceClient's GetPodLogs to the
// enterprise-typed PodLogsReader consumed by the stale-disk wipe core.
func ossPodLogsReader(client *lifecycle.ResourceClient[lifecycle.ClusterWithPools, *lifecycle.ClusterWithPools]) entcontroller.PodLogsReader {
	return func(ctx context.Context, pod *entlifecycle.MulticlusterPod, opts *corev1.PodLogOptions) (string, error) {
		return client.GetPodLogs(ctx, fromEnterprisePod(pod), opts)
	}
}
