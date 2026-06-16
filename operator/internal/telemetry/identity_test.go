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
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func identityScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	return scheme
}

func TestResolveSourceID_WalksToDeployment(t *testing.T) {
	scheme := identityScheme(t)

	deploymentUID := types.UID("deployment-uid")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-pod",
			Namespace: "redpanda",
			UID:       types.UID("pod-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "operator-rs",
				UID:        types.UID("rs-uid"),
				Controller: ptr.To(true),
			}},
		},
	}

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-rs",
			Namespace: "redpanda",
			UID:       types.UID("rs-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "operator",
				UID:        deploymentUID,
				Controller: ptr.To(true),
			}},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, rs).Build()

	t.Setenv("POD_NAMESPACE", "redpanda")
	t.Setenv("POD_NAME", "operator-pod")

	require.Equal(t, string(deploymentUID), ResolveSourceID(t.Context(), c))
}

func TestResolveSourceID_FallsBackToPodUID(t *testing.T) {
	scheme := identityScheme(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-pod",
			Namespace: "redpanda",
			UID:       types.UID("pod-uid"),
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()

	t.Setenv("POD_NAMESPACE", "redpanda")
	t.Setenv("POD_NAME", "operator-pod")

	require.Equal(t, "pod-uid", ResolveSourceID(t.Context(), c))
}

func TestResolveSourceID_MissingEnv(t *testing.T) {
	scheme := identityScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	t.Setenv("POD_NAMESPACE", "")
	t.Setenv("POD_NAME", "")

	require.Equal(t, "", ResolveSourceID(t.Context(), c))
}
