// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package finalizerremoval contains a pre-delete job that removes finalizers
// from all operator-managed CRs to allow clean uninstall of the operator.
package finalizerremoval

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
)

const finalizerKey = "operator.redpanda.com/finalizer"

// operatorGroups are the API groups whose resources we manage and may have finalizers.
var operatorGroups = []string{
	"cluster.redpanda.com",
	"redpanda.vectorized.io",
}

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

	// Discover all non-list GVKs registered in our scheme for the operator groups.
	// This automatically picks up any new types added in future without requiring
	// changes to this command.
	var gvks []schema.GroupVersionKind
	for gvk := range controller.UnifiedScheme.AllKnownTypes() {
		if strings.HasSuffix(gvk.Kind, "List") {
			continue
		}
		for _, g := range operatorGroups {
			if gvk.Group == g {
				gvks = append(gvks, gvk)
				break
			}
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
