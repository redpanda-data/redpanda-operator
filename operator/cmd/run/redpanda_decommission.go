// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	"context"
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	pkglabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// redpandaDecommissionerAdapter maps chart-rendered StatefulSets back to their
// owning v1alpha2 Redpanda CR so the StatefulSetDecommissioner can run
// operator-wide against V2 (Redpanda / chart-based) clusters. It mirrors
// vectorizedDecommissionerAdapter (the V1 ghost-broker path) but resolves the
// V2 Redpanda resource and builds its admin client through the shared factory,
// which derives the admin endpoints from the chart-rendered DNS rather than the
// helm release name (avoiding the olddecommission fullnameOverride DNS bug).
type redpandaDecommissionerAdapter struct {
	client  client.Client
	factory internalclient.ClientFactory
}

// getRedpanda resolves the Redpanda CR that owns sts via its
// app.kubernetes.io/instance label. It returns (nil, nil) when the StatefulSet
// is not a Redpanda-managed one (no/unknown instance label) so callers can skip
// it without treating that as an error.
func (b *redpandaDecommissionerAdapter) getRedpanda(ctx context.Context, sts *appsv1.StatefulSet) (*redpandav1alpha2.Redpanda, error) {
	instance, ok := sts.Labels[pkglabels.InstanceKey]
	if !ok || instance == "" {
		return nil, nil
	}

	var redpanda redpandav1alpha2.Redpanda
	if err := b.client.Get(ctx, types.NamespacedName{Name: instance, Namespace: sts.Namespace}, &redpanda); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "could not get Redpanda")
	}

	return &redpanda, nil
}

// desiredReplicas returns the cluster-wide desired broker count: the sum of
// every NodePool StatefulSet's replicas. With NodePools a cluster spans several
// StatefulSets, so the "are there excess brokers?" gate must compare against the
// sum, not a single StatefulSet's replicas.
func (b *redpandaDecommissionerAdapter) desiredReplicas(ctx context.Context, sts *appsv1.StatefulSet) (int32, error) {
	redpanda, err := b.getRedpanda(ctx, sts)
	if err != nil {
		return 0, err
	}
	if redpanda == nil {
		return 0, errors.Newf("failed to resolve %s/%s to a Redpanda cluster", sts.Namespace, sts.Name)
	}

	var stsList appsv1.StatefulSetList
	if err := b.client.List(ctx, &stsList,
		client.InNamespace(redpanda.Namespace),
		client.MatchingLabels{pkglabels.InstanceKey: redpanda.Name},
	); err != nil {
		return 0, fmt.Errorf("failed to list StatefulSets of Redpanda %q: %w", redpanda.Name, err)
	}
	if len(stsList.Items) == 0 {
		return 0, errors.Newf("found 0 StatefulSets for Redpanda %q", redpanda.Name)
	}

	var total int32
	for i := range stsList.Items {
		total += ptr.Deref(stsList.Items[i].Spec.Replicas, 0)
	}
	return total, nil
}

// filter gates reconciliation to Redpanda-managed StatefulSets that are not
// being torn down.
//
// It deliberately does NOT gate on the Redpanda's Ready/Quiesced conditions or
// observed generation: a scale-down leaves the cluster non-Ready (and its
// generation unobserved) precisely until the excess broker is decommissioned,
// so a readiness gate would deadlock the very operation this controller exists
// to perform. Guarding against premature action is the decommissioner's own
// job — it reads live cluster health from the admin API and only decommissions
// when the registered broker count exceeds the summed desired replicas.
func (b *redpandaDecommissionerAdapter) filter(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	log := ctrl.LoggerFrom(ctx, "namespace", sts.Namespace).WithName("StatefulSetDecomissioner.Filter")

	redpanda, err := b.getRedpanda(ctx, sts)
	if err != nil {
		return false, err
	}
	if redpanda == nil {
		return false, nil
	}

	if redpanda.DeletionTimestamp != nil {
		log.V(1).Info("Redpanda is being deleted; skipping", "redpanda", redpanda.Name)
		return false, nil
	}

	return true, nil
}

// getAdminClient builds an admin API client targeting the Redpanda cluster that
// owns sts. The factory derives endpoints from the Redpanda CR's chart-rendered
// DNS.
func (b *redpandaDecommissionerAdapter) getAdminClient(ctx context.Context, sts *appsv1.StatefulSet) (*rpadmin.AdminAPI, error) {
	redpanda, err := b.getRedpanda(ctx, sts)
	if err != nil {
		return nil, err
	}
	if redpanda == nil {
		return nil, errors.Newf("failed to resolve %s/%s to a Redpanda cluster", sts.Namespace, sts.Name)
	}

	return b.factory.RedpandaAdminClient(ctx, redpanda)
}
