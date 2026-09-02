// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	// Injected bound service account token expiration which triggers monitoring of its time-bound feature.
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/ae53151cb4e6fbba8bb78a2ef0b48a7c32a0a067/pkg/serviceaccount/claims.go#L38-L39
	tokenExpirationSeconds = 60*60 + 7

	// ServiceAccountVolumeName is the prefix name that will be added to volumes that mount ServiceAccount secrets
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L52-L53
	ServiceAccountVolumeName = "kube-api-access"

	// DefaultAPITokenMountPath is the path that ServiceAccountToken secrets are automounted to.
	// The token file would then be accessible at /var/run/secrets/kubernetes.io/serviceaccount
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L55-L57
	//nolint:gosec
	DefaultAPITokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
)

// KubeTokenAPIVolume is a slightly changed variant of
// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L484-L524
// Upstream creates Projected Volume Source, but this function returns Volume with provided name.
// Also const are renamed.
func KubeTokenAPIVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				// explicitly set default value, see https://github.com/kubernetes/kubernetes/issues/104464
				DefaultMode: ptr.To(corev1.ProjectedVolumeSourceDefaultMode),
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: ptr.To(int64(tokenExpirationSeconds)),
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kube-root-ca.crt",
							},
							Items: []corev1.KeyToPath{
								{
									Key:  "ca.crt",
									Path: "ca.crt",
								},
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "namespace",
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
