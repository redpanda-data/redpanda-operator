// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package telemetry

import (
	"context"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveSourceID returns the operator Deployment's UID by walking
// Pod -> ReplicaSet -> Deployment owner references. Best effort: falls back
// to the pod UID, then "".
//
// It takes a client.Reader (not the manager's cached client) and is meant to be
// called with mgr.GetAPIReader() at startup, before mgr.Start(): the cached
// client returns ErrCacheNotStarted for pre-start reads, and its lazily-started
// informers would also require list+watch RBAC. An uncached APIReader reads
// directly from the API server, works pre-start, and needs only "get".
func ResolveSourceID(ctx context.Context, reader client.Reader) string {
	namespace := os.Getenv("POD_NAMESPACE")
	name := os.Getenv("POD_NAME")
	if namespace == "" || name == "" {
		return ""
	}

	var pod corev1.Pod
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pod); err != nil {
		return ""
	}

	rsRef := controllerOwnerOfKind(pod.OwnerReferences, "ReplicaSet")
	if rsRef == nil {
		return string(pod.UID)
	}

	var rs appsv1.ReplicaSet
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rsRef.Name}, &rs); err != nil {
		return string(pod.UID)
	}

	if deployRef := controllerOwnerOfKind(rs.OwnerReferences, "Deployment"); deployRef != nil {
		return string(deployRef.UID)
	}

	return string(pod.UID)
}

func controllerOwnerOfKind(refs []metav1.OwnerReference, kind string) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Kind == kind && refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}
