// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_serviceaccount.go.tpl
package multicluster

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func CRDJobServiceAccountName(dot *helmette.Dot) string {
	return Fullname(dot) + "-crd-job"
}

// Create the name of the service account to use
func ServiceAccountName(dot *helmette.Dot) string {
	return Fullname(dot)
}

func ServiceAccount(dot *helmette.Dot) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName(dot),
			Labels:    Labels(dot),
			Namespace: dot.Release.Namespace,
		},
	}
}

// CRDJobServiceAccount returns a ServiceAccount that's used by
// [PreInstallCRDJob]. Helm will delete it after the job succeeds.
func CRDJobServiceAccount(dot *helmette.Dot) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CRDJobServiceAccountName(dot),
			Labels:    Labels(dot),
			Namespace: dot.Release.Namespace,
			Annotations: map[string]string{
				"helm.sh/hook":               "pre-install,pre-upgrade",
				"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
				"helm.sh/hook-weight":        "-10",
			},
		},
	}
}
