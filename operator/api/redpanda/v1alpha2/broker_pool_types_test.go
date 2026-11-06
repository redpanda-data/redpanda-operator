// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func testBrokerPoolValidation(t *testing.T, c client.Client, ns string) {
	ctx := t.Context()

	basePool := RedpandaBrokerPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: ns,
		},
		Spec: BrokerPoolSpec{
			ClusterRef: ClusterRef{Name: "cluster"},
			EmbeddedBrokerPoolSpec: EmbeddedBrokerPoolSpec{
				Replicas: ptr.To(int32(3)),
			},
		},
	}
	stretchOnly := []string{"only supported when clusterref references a stretchcluster"}

	for name, tt := range map[string]validationTestCase[*RedpandaBrokerPool]{
		"redpanda pool": {},
		"redpanda pool with stretch-only fields": {
			mutate: func(pool *RedpandaBrokerPool) {
				pool.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "creds"}}
			},
			errors: stretchOnly,
		},
		"explicit redpanda kind with stretch-only fields": {
			mutate: func(pool *RedpandaBrokerPool) {
				pool.Spec.ClusterRef.Kind = ptr.To(RedpandaKind)
				pool.Spec.ClusterDomain = ptr.To("cluster.local")
			},
			errors: stretchOnly,
		},
		"stretch pool with stretch-only fields": {
			mutate: func(pool *RedpandaBrokerPool) {
				pool.Spec.ClusterRef.Kind = ptr.To(StretchClusterRefKind)
				pool.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "creds"}}
				pool.Spec.ClusterDomain = ptr.To("cluster.local")
			},
		},
		"adding stretch-only fields to a redpanda pool": {
			mutateUpdate: func(pool *RedpandaBrokerPool) {
				pool.Spec.ClusterDomain = ptr.To("cluster.local")
			},
			updateErrors: stretchOnly,
		},
	} {
		t.Run(name, func(t *testing.T) {
			runValidationTest(ctx, t, tt, c, &basePool)
		})
	}
}
