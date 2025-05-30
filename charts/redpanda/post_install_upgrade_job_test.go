// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func TestPostInstallUpgradeEnvironmentVariables(t *testing.T) {
	tests := []struct {
		name            string
		values          Values
		expectedEnvVars []corev1.EnvVar
	}{
		{
			"empty-result",
			Values{Storage: Storage{Tiered: Tiered{}}},
			[]corev1.EnvVar{},
		},
		{
			"only-literal-license",
			Values{
				Storage:    Storage{Tiered: Tiered{}},
				Enterprise: Enterprise{License: "fake.license"},
			},
			[]corev1.EnvVar{{Name: "REDPANDA_LICENSE", Value: "fake.license"}},
		},
		{
			name: "only-secret-ref-license",
			values: Values{
				Storage: Storage{Tiered: Tiered{}},
				Enterprise: Enterprise{LicenseSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "some-secret",
					},
					Key: "some-key",
				}},
			},
			expectedEnvVars: []corev1.EnvVar{{Name: "REDPANDA_LICENSE", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
					Key:                  "some-key",
				},
			}}},
		},
		{
			name: "azure-literal-shared-key",
			values: Values{
				Storage: Storage{Tiered: Tiered{
					Config: TieredStorageConfig{
						"cloud_storage_enabled":               true,
						"cloud_storage_azure_shared_key":      "fake-shared-key",
						"cloud_storage_azure_container":       "fake-azure-container",
						"cloud_storage_azure_storage_account": "fake-storage-account",
					},
				}},
			},
			expectedEnvVars: []corev1.EnvVar{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.values)
			require.NoError(t, err)
			dot := helmette.Dot{}
			err = json.Unmarshal(b, &dot.Values)
			require.NoError(t, err)

			envVars := PostInstallUpgradeEnvironmentVariables(&dot)

			slices.SortFunc(envVars, compareEnvVars)
			slices.SortFunc(tc.expectedEnvVars, compareEnvVars)
			require.Equal(t, tc.expectedEnvVars, envVars)
		})
	}
}

func compareEnvVars(a, b corev1.EnvVar) int {
	if a.Name < b.Name {
		return -1
	} else {
		return 1
	}
}

func TestAnnotationsOverwrite(t *testing.T) {
	v := PartialValues{
		PostInstallJob: &PartialPostInstallJob{
			Enabled: ptr.To(true),
			Annotations: map[string]string{
				"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded",
			},
			Labels: map[string]string{
				"app.kubernetes.io/name": "overwrite-name",
			},
			PodTemplate: &PartialPodTemplate{
				Labels: map[string]string{
					"app.kubernetes.io/name": "overwrite-pod-template-name",
				},
				Annotations: map[string]string{
					"some-annotation": "some-annotation-value",
				},
			},
		},
	}

	dot, err := Chart.Dot(nil, helmette.Release{}, v)
	require.NoError(t, err)

	job := PostInstallUpgradeJob(dot)
	require.Equal(t, job.Annotations["helm.sh/hook-delete-policy"], "before-hook-creation,hook-succeeded")
	require.Equal(t, job.Labels["app.kubernetes.io/name"], "overwrite-name")
	require.Equal(t, job.Spec.Template.Annotations["some-annotation"], "some-annotation-value")
	require.Equal(t, job.Spec.Template.Labels["app.kubernetes.io/name"], "overwrite-pod-template-name")
}
