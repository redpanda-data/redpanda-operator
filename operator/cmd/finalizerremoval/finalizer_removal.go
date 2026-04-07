// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package finalizerremoval contains a post-delete job that removes finalizers
// from all operator-managed CRs to allow clean uninstall of the operator.
package finalizerremoval

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
)

const finalizerKey = "operator.redpanda.com/finalizer"

func Command() *cobra.Command {
	return &cobra.Command{
		Use:   "finalizer-removal",
		Short: "Remove finalizers from operator-managed CRs to allow clean uninstall",
		Run: func(cmd *cobra.Command, args []string) {
			run(cmd.Context())
		},
	}
}

func run(ctx context.Context) {
	log.Printf("Removing finalizers from operator-managed CRs")

	k8sClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: controller.UnifiedScheme})
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to create client: %w", err))
	}

	// Derive GVKs from the embedded CRD definitions. This automatically picks
	// up any new types added in future without requiring changes to this command
	// and avoids iterating over primitive Kubernetes types in the scheme.
	var gvks []schema.GroupVersionKind
	for _, crd := range crds.All() {
		for _, v := range crd.Spec.Versions {
			gvks = append(gvks, schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Version: v.Name,
				Kind:    crd.Spec.Names.Kind,
			})
		}
	}

	var errs []error
	for _, gvk := range gvks {
		errs = append(errs, removeFinalizersForGVK(ctx, k8sClient, gvk))
	}

	if err := errors.Join(errs...); err != nil {
		log.Fatalf("%s", fmt.Errorf("errors while removing finalizers: %w", err))
	}

	log.Printf("Finalizer removal complete")
}

func removeFinalizersForGVK(ctx context.Context, k8sClient client.Client, gvk schema.GroupVersionKind) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind + "List",
	})

	if err := k8sClient.List(ctx, list); err != nil {
		// If the CRD isn't installed, the API server returns a "no kind is registered"
		// or "resource not found" error. Log it and continue rather than failing.
		log.Printf("skipping %v: %v", gvk, err)
		return nil
	}

	var errs []error
	for i := range list.Items {
		obj := &list.Items[i]
		if !controllerutil.ContainsFinalizer(obj, finalizerKey) {
			continue
		}
		patch := client.MergeFrom(obj.DeepCopy())
		controllerutil.RemoveFinalizer(obj, finalizerKey)
		if err := k8sClient.Patch(ctx, obj, patch); err != nil {
			errs = append(errs, fmt.Errorf("patch %v %s/%s: %w", gvk, obj.GetNamespace(), obj.GetName(), err))
		} else {
			log.Printf("removed finalizer from %v %s/%s", gvk, obj.GetNamespace(), obj.GetName())
		}
	}
	return errors.Join(errs...)
}
