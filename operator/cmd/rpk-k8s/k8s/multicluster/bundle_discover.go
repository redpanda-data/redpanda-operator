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
	"context"
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// peerDiscovery is the result of looking up cache Secrets on a starting
// cluster and turning each one into a peer ClusterConnection.
type peerDiscovery struct {
	// Connections are the peers built from labelled cache Secrets, in
	// arbitrary order (apiserver list order). Excludes the starting cluster.
	Connections []ClusterConnection
	// Warnings are non-fatal issues encountered during discovery (e.g. a
	// malformed cache Secret); the bundle proceeds with whatever peers were
	// discoverable. Each entry is a complete sentence suitable for errors.txt.
	Warnings []string
}

// discoverPeers lists peer-kubeconfig cache Secrets in `namespace` of the
// starting cluster (selected by the `app.kubernetes.io/component=
// multicluster-kubeconfig-cache` label) and builds a ClusterConnection for
// each. The peer name is read from the MulticlusterPeerLabel rather than
// derived from the Secret name, so this works regardless of the operator's
// configured --kubeconfig-name prefix.
//
// Errors that prevent listing the Secrets at all are returned. Per-Secret
// failures (missing kubeconfig.yaml key, malformed kubeconfig, etc.) are
// recorded in peerDiscovery.Warnings so the bundle can still cover the
// peers that were discoverable.
func discoverPeers(ctx context.Context, ctl *kube.Ctl, namespace string) (peerDiscovery, error) {
	var list corev1.SecretList
	if err := ctl.List(ctx, namespace, &list, client.MatchingLabels{
		multicluster.KubeconfigCacheComponentLabel: multicluster.KubeconfigCacheComponentValue,
	}); err != nil {
		return peerDiscovery{}, fmt.Errorf("listing kubeconfig cache Secrets in %q: %w", namespace, err)
	}

	var out peerDiscovery
	for _, s := range list.Items {
		peerName := s.Labels[multicluster.MulticlusterPeerLabel]
		if peerName == "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"Secret %s/%s is missing the %s label, skipping",
				s.Namespace, s.Name, multicluster.MulticlusterPeerLabel,
			))
			continue
		}

		raw, ok := s.Data["kubeconfig.yaml"]
		if !ok || len(raw) == 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"Secret %s/%s is missing the kubeconfig.yaml data key, skipping peer %q",
				s.Namespace, s.Name, peerName,
			))
			continue
		}

		rc, err := multicluster.LoadKubeconfigFromBytes(raw)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"parsing kubeconfig from Secret %s/%s for peer %q: %v",
				s.Namespace, s.Name, peerName, err,
			))
			continue
		}

		peerCtl, err := kube.FromRESTConfig(rc)
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"building client for peer %q from Secret %s/%s: %v",
				peerName, s.Namespace, s.Name, err,
			))
			continue
		}

		out.Connections = append(out.Connections, ClusterConnection{
			Name:         peerName,
			Ctl:          peerCtl,
			SecretPrefix: peerName,
		})
	}

	return out, nil
}
