// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_serviceaccount.go.tpl
package operator

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Create the name of the service account to use
func ServiceAccountName(dot *helmette.Dot) string {
	values := helmette.Unwrap[Values](dot.Values)
	return ptr.Deref(values.ServiceAccount.Name, Fullname(dot))
}

func CRDJobServiceAccountName(dot *helmette.Dot) string {
	return ServiceAccountName(dot) + "-crd-job"
}

func ServiceAccount(dot *helmette.Dot) *corev1.ServiceAccount {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.ServiceAccount.Create {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ServiceAccountName(dot),
			Labels:      Labels(dot),
			Namespace:   dot.Release.Namespace,
			Annotations: values.ServiceAccount.Annotations,
		},
		AutomountServiceAccountToken: values.ServiceAccount.AutomountServiceAccountToken,
	}
}

// CRDJobServiceAccount returns a ServiceAccount that's used by
// [PreInstallCRDJob]. Helm will delete it after the job succeeds.
func CRDJobServiceAccount(dot *helmette.Dot) *corev1.ServiceAccount {
	values := helmette.Unwrap[Values](dot.Values)

	if !(values.CRDs.Enabled || values.CRDs.Experimental) {
		return nil
	}

	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CRDJobServiceAccountName(dot),
			Labels:    Labels(dot),
			Namespace: dot.Release.Namespace,
			Annotations: helmette.Merge(
				helmette.Default(
					map[string]string{},
					values.ServiceAccount.Annotations,
				),
				map[string]string{
					"helm.sh/hook":               "pre-install,pre-upgrade",
					"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded,hook-failed",
					"helm.sh/hook-weight":        "-10",
				},
			),
		},
		AutomountServiceAccountToken: ptr.To(false),
	}
}
