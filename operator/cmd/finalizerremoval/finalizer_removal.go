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

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
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

	var errs []error

	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.RedpandaRoleList{}, func(l *redpandav1alpha2.RedpandaRoleList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.UserList{}, func(l *redpandav1alpha2.UserList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.GroupList{}, func(l *redpandav1alpha2.GroupList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.TopicList{}, func(l *redpandav1alpha2.TopicList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.SchemaList{}, func(l *redpandav1alpha2.SchemaList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.ShadowLinkList{}, func(l *redpandav1alpha2.ShadowLinkList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.RedpandaList{}, func(l *redpandav1alpha2.RedpandaList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))
	errs = append(errs, removeFinalizersForType(ctx, k8sClient, &redpandav1alpha2.NodePoolList{}, func(l *redpandav1alpha2.NodePoolList) []client.Object {
		out := make([]client.Object, len(l.Items))
		for i := range l.Items {
			out[i] = &l.Items[i]
		}
		return out
	}))

	if err := errors.Join(errs...); err != nil {
		log.Fatalf("%s", fmt.Errorf("errors while removing finalizers: %w", err))
	}

	log.Printf("Finalizer removal complete")
}

func removeFinalizersForType[L client.ObjectList](ctx context.Context, k8sClient client.Client, list L, items func(L) []client.Object) error {
	if err := k8sClient.List(ctx, list); err != nil {
		// If the CRD isn't installed the API server returns a "no kind registered" or
		// "resource not found" error. Log it and move on rather than failing the whole job.
		log.Printf("skipping %T: %v", list, err)
		return nil
	}

	var errs []error
	for _, obj := range items(list) {
		if !controllerutil.ContainsFinalizer(obj, finalizerKey) {
			continue
		}
		patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
		controllerutil.RemoveFinalizer(obj, finalizerKey)
		if err := k8sClient.Patch(ctx, obj, patch); err != nil {
			errs = append(errs, fmt.Errorf("patch %T %s/%s: %w", obj, obj.GetNamespace(), obj.GetName(), err))
		} else {
			log.Printf("removed finalizer from %T %s/%s", obj, obj.GetNamespace(), obj.GetName())
		}
	}
	return errors.Join(errs...)
}
