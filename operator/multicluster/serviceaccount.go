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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func serviceAccount(state *RenderState) *corev1.ServiceAccount {
	sa := state.Spec().ServiceAccount
	if !sa.ShouldCreate() {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        state.Spec().GetServiceAccountName(state.fullname()),
			Namespace:   state.namespace,
			Labels:      state.commonLabels(),
			Annotations: sa.Annotations,
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}
