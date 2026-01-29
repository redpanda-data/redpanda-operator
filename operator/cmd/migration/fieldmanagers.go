// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package migration

import (
	"context"
	"slices"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

var undesiredFieldManagers = []string{
	"*kube.Ctl",
}

// this is a potentially heavy operation
func migrateFieldManagers(ctx context.Context, ctl *kube.Ctl, k8sClient client.Client) error {
	var redpandas redpandav1alpha2.RedpandaList
	if err := k8sClient.List(ctx, &redpandas); err != nil {
		return err
	}

	var consoles redpandav1alpha2.ConsoleList
	if err := k8sClient.List(ctx, &consoles); err != nil {
		return err
	}

	redpandaTypes := redpandachart.Types()
	consoleTypes := consolechart.Types()
	ownershipResolver := lifecycle.NewV2OwnershipResolver()

	for _, rp := range redpandas.Items {
		if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, &rp); err != nil {
			return err
		}

		// get the ownership labels for Redpanda-owned resources
		labels := ownershipResolver.GetOwnerLabels(&lifecycle.ClusterWithPools{
			Redpanda: &rp,
		})
		for _, rt := range redpandaTypes {
			resources, err := listIfResourceExists(ctx, k8sClient, labels, &rp, rt)
			if err != nil {
				return err
			}
			for _, resource := range resources {
				if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, resource); err != nil {
					return err
				}
			}
		}
	}

	for _, console := range consoles.Items {
		if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, &console); err != nil {
			return err
		}

		// get ownership labels for the Console controller
		labels := consoleOwnershipLabels(&console)
		for _, rt := range consoleTypes {
			resources, err := listIfResourceExists(ctx, k8sClient, labels, &console, rt)
			if err != nil {
				return err
			}
			for _, resource := range resources {
				if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, resource); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// copied from operator/internal/controller/console/controller.go
func consoleOwnershipLabels(console *redpandav1alpha2.Console) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       consolechart.ChartName,
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/instance":   console.Name,
	}
}

func maybeUpdate(ctx context.Context, undesiredManagers []string, ctl *kube.Ctl, k8sClient client.Client, obj client.Object) error {
	if !removeFieldManagers(undesiredManagers, obj) {
		return nil
	}

	managers := obj.GetManagedFields()
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				// resource was deleted, just skip
				return nil
			}
			return err
		}
		obj.SetManagedFields(managers)
		return k8sClient.Update(ctx, obj)
	}); err != nil {
		return err
	}

	// now we do a fetch + server-side apply to make sure that our field manager owns any
	// fields that it should in-case anything was orphaned by the removal above
	return ctl.Apply(ctx, obj, client.ForceOwnership)
}

func removeFieldManagers(undesiredManagers []string, obj client.Object) bool {
	managers := obj.GetManagedFields()
	updated := []metav1.ManagedFieldsEntry{}
	changed := false
	for _, manager := range managers {
		if slices.Contains(undesiredManagers, manager.Manager) {
			changed = true
			continue
		}
		updated = append(updated, manager)
	}
	if changed {
		obj.SetManagedFields(updated)
	}
	return changed
}

// this logic is roughly Syncer.listInPurview from the kube package
func listIfResourceExists(ctx context.Context, k8sClient client.Client, labels map[string]string, owner client.Object, objectType client.Object) ([]client.Object, error) {
	gvk, err := kube.GVKFor(k8sClient.Scheme(), objectType)
	if err != nil {
		return nil, err
	}

	mapping, err := k8sClient.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		// If we encounter an unknown type, then just return nil and skip it
		if meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}

	list, err := kube.ListFor(k8sClient.Scheme(), objectType)
	if err != nil {
		return nil, err
	}

	if err := k8sClient.List(ctx, list, client.InNamespace(owner.GetNamespace()), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	items, err := kube.Items[client.Object](list)
	if err != nil {
		return nil, err
	}

	// if we're in the namespace scope, filter by owner references
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		filtered := []client.Object{}
		for _, obj := range items {
			owned := slices.ContainsFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
				return ref.UID == owner.GetUID()
			})

			if owned {
				filtered = append(filtered, obj)
			}
		}
		return filtered, nil
	}

	return items, nil
}
