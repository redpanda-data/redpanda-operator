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
	"fmt"
	"log"
	"slices"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consolechart "github.com/redpanda-data/redpanda-operator/charts/console/v3"
	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
)

var undesiredFieldManagers = []string{
	"*kube.Ctl",
	"helm-controller",
	"helm",
	"redpanda-operator",
	"redpanda-controller",
	"redpanda-helmrelease-controller",
	"application/apply-patch",
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

	// Forbidden errors below this point are non-fatal. Installs that manage
	// RBAC by hand (rbac.create=false) often lack permissions for optional
	// resource types they don't use — e.g. Gateway API TLSRoutes, swept only
	// because the CRDs happen to be installed — and failing the post-upgrade
	// hook over them wedges every subsequent upgrade. Anything skipped here
	// is re-swept on the next upgrade once the permission is granted. Only
	// the Redpanda/Console lists above stay fatal: if we can't see the CRs
	// this job exists to serve, something is fundamentally broken.
	warnedTypes := map[string]bool{}

	for _, rp := range redpandas.Items {
		if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, &rp); err != nil {
			if !apierrors.IsForbidden(err) {
				return err
			}
			warnForbiddenUpdate(k8sClient.Scheme(), &rp, err)
		}

		// get the ownership labels for Redpanda-owned resources
		labels := ownershipResolver.GetOwnerLabels(&lifecycle.ClusterWithPools{
			Redpanda: &rp,
		})
		for _, rt := range redpandaTypes {
			resources, err := listIfResourceExists(ctx, k8sClient, labels, &rp, rt)
			if err != nil {
				if !apierrors.IsForbidden(err) {
					return err
				}
				warnForbiddenList(k8sClient.Scheme(), warnedTypes, rt, err)
				continue
			}
			for _, resource := range resources {
				if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, resource); err != nil {
					if !apierrors.IsForbidden(err) {
						return err
					}
					warnForbiddenUpdate(k8sClient.Scheme(), resource, err)
				}
			}
		}
	}

	for _, console := range consoles.Items {
		if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, &console); err != nil {
			if !apierrors.IsForbidden(err) {
				return err
			}
			warnForbiddenUpdate(k8sClient.Scheme(), &console, err)
		}

		// get ownership labels for the Console controller
		labels := consoleOwnershipLabels(&console)
		for _, rt := range consoleTypes {
			resources, err := listIfResourceExists(ctx, k8sClient, labels, &console, rt)
			if err != nil {
				if !apierrors.IsForbidden(err) {
					return err
				}
				warnForbiddenList(k8sClient.Scheme(), warnedTypes, rt, err)
				continue
			}
			for _, resource := range resources {
				if err := maybeUpdate(ctx, undesiredFieldManagers, ctl, k8sClient, resource); err != nil {
					if !apierrors.IsForbidden(err) {
						return err
					}
					warnForbiddenUpdate(k8sClient.Scheme(), resource, err)
				}
			}
		}
	}

	return nil
}

// warnForbiddenList records that the sweep is skipping a resource type the
// migration job's ServiceAccount cannot list. Logged once per type: one
// missing rule would otherwise repeat for every Redpanda/Console CR. The
// sweep still runs for each CR's namespace, so namespace-scoped grants that
// cover only some namespaces migrate what they can.
func warnForbiddenList(scheme *runtime.Scheme, warned map[string]bool, objectType client.Object, err error) {
	name := typeName(scheme, objectType)
	if warned[name] {
		return
	}
	warned[name] = true
	log.Printf("WARNING: skipping %s: %v — grant the missing permission to the migration job's ServiceAccount, or ignore this warning if you don't use this resource type", name, err)
}

// warnForbiddenUpdate records that a resource keeps its current field
// managers because the migration job's ServiceAccount may not update it.
// Logged per resource rather than per type: unlike a skipped list, these are
// resources the operator actively manages.
func warnForbiddenUpdate(scheme *runtime.Scheme, obj client.Object, err error) {
	log.Printf("WARNING: cannot migrate field managers of %s %s: %v — the resource keeps its current field managers until the next upgrade after the migration job's ServiceAccount is granted the missing permission", typeName(scheme, obj), client.ObjectKeyFromObject(obj), err)
}

func typeName(scheme *runtime.Scheme, obj client.Object) string {
	if gvk, err := kube.GVKFor(scheme, obj); err == nil {
		return gvk.GroupKind().String()
	}
	return fmt.Sprintf("%T", obj)
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

	// deep-copy as paranoia for potential overwrite during retry loop
	managers := obj.DeepCopyObject().(client.Object).GetManagedFields()
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
