// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_serviceaccount.go.tpl
package redpanda

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// Create the name of the service account to use
func ServiceAccountName(state *RenderState) string {
	serviceAccount := state.Values.ServiceAccount

	if serviceAccount.Create && serviceAccount.Name != "" {
		return serviceAccount.Name
	} else if serviceAccount.Create {
		return Fullname(state)
	} else if serviceAccount.Name != "" {
		return serviceAccount.Name
	}

	return "default"
}

func ServiceAccount(state *RenderState) *corev1.ServiceAccount {
	if !state.Values.ServiceAccount.Create {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceAccountName(state),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: state.Values.ServiceAccount.Annotations,
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}
