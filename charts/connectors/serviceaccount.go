// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_serviceaccount.go.tpl
package connectors

import (
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ServiceAccount(dot *helmette.Dot) *corev1.ServiceAccount {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.ServiceAccount.Create {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: values.ServiceAccount.Annotations,
			Labels:      FullLabels(dot),
			Name:        ServiceAccountName(dot),
			Namespace:   dot.Release.Namespace,
		},
		AutomountServiceAccountToken: values.ServiceAccount.AutomountServiceAccountToken,
	}
}