// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Create the name of the service account to use
func ServiceAccountName(state *RenderState) string {
	if state.Values.ServiceAccount.Create {
		if state.Values.ServiceAccount.Name != "" {
			return state.Values.ServiceAccount.Name
		}
		return state.FullName()
	}

	return helmette.Default("default", state.Values.ServiceAccount.Name)
}

func ServiceAccount(state *RenderState) *corev1.ServiceAccount {
	if !state.Values.ServiceAccount.Create {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceAccountName(state),
			Labels:      state.Labels(nil),
			Namespace:   state.Namespace,
			Annotations: state.Values.ServiceAccount.Annotations,
		},
		AutomountServiceAccountToken: ptr.To(state.Values.ServiceAccount.AutomountServiceAccountToken),
	}
}
