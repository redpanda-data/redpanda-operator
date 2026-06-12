// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/fuzzing"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/rapidutil"
)

func TestNodepoolConversion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		values := rapid.MakeCustom[redpanda.Values](rapidutil.KubernetesTypes).Draw(t, "values")
		pools := rapid.SliceOf(rapid.MakeCustom[redpandav1alpha2.NodePool](rapidutil.KubernetesTypes)).Draw(t, "pools")
		_, err := convertV2NodepoolsToPools(values, functional.MapFn(ptr.To, pools), &V2Defaulters{})
		require.NoError(t, err)
	})
}

// TestPersistentVolumeClaimRetentionPolicyPrecedence pins the precedence chain for
// persistentVolumeClaimRetentionPolicy: NodePool override > Redpanda CR cluster-level
// value > chart default. The chain is implicit in the order of convertV2Fields and
// convertV2NodepoolsToPools, so this test guards against silent regressions if those
// are re-ordered.
func TestPersistentVolumeClaimRetentionPolicyPrecedence(t *testing.T) {
	deletePolicy := func(scaled, deleted appsv1.PersistentVolumeClaimRetentionPolicyType) *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy {
		return &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
			WhenScaled:  scaled,
			WhenDeleted: deleted,
		}
	}
	clusterPolicy := deletePolicy(appsv1.DeletePersistentVolumeClaimRetentionPolicyType, appsv1.RetainPersistentVolumeClaimRetentionPolicyType)
	poolPolicy := deletePolicy(appsv1.RetainPersistentVolumeClaimRetentionPolicyType, appsv1.DeletePersistentVolumeClaimRetentionPolicyType)

	cases := []struct {
		name        string
		clusterSet  *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy
		poolSet     *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy
		wantCluster appsv1.PersistentVolumeClaimRetentionPolicyType
		wantScaled  appsv1.PersistentVolumeClaimRetentionPolicyType
		wantDeleted appsv1.PersistentVolumeClaimRetentionPolicyType
	}{
		{
			name:        "neither set -> chart default Retain/Retain",
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "cluster only -> pool inherits cluster",
			clusterSet:  clusterPolicy,
			wantScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "pool only -> pool wins over chart default",
			poolSet:     poolPolicy,
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		},
		{
			name:        "pool overrides cluster",
			clusterSet:  clusterPolicy,
			poolSet:     poolPolicy,
			wantScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
			wantDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Run convertV2Fields to populate the cluster-level value into state.Values,
			// mirroring what ConvertV2ToRenderState does before per-pool conversion.
			values := redpanda.Values{}
			clusterSpec := &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					PersistentVolumeClaimRetentionPolicy: tc.clusterSet,
				},
			}
			state := &redpanda.RenderState{Values: values}
			require.NoError(t, convertV2Fields(state, &state.Values, clusterSpec))

			pool := &redpandav1alpha2.NodePool{
				Spec: redpandav1alpha2.NodePoolSpec{
					EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{
						PersistentVolumeClaimRetentionPolicy: tc.poolSet,
					},
				},
			}
			converted, err := convertV2NodepoolToPool(state.Values, pool, &V2Defaulters{})
			require.NoError(t, err)
			got := converted.Statefulset.PersistentVolumeClaimRetentionPolicy
			require.NotNil(t, got, "rendered pool should always carry a policy (chart default ensures this)")
			require.Equal(t, tc.wantScaled, got.WhenScaled)
			require.Equal(t, tc.wantDeleted, got.WhenDeleted)
		})
	}
}

// TestConvertStatefulsetV2FieldsBrokerContainer is a regression test for
// https://github.com/redpanda-data/redpanda-operator/issues/1577:
// convertStatefulsetV2Fields retained a pointer to the redpanda container
// across the append that adds the sidecar container. The append reallocated
// the slice's backing array, so extraVolumeMounts, livenessProbe, and
// startupProbe were written to an orphaned copy and silently dropped from
// the broker container — they only ever landed on the sidecar.
func TestConvertStatefulsetV2FieldsBrokerContainer(t *testing.T) {
	dot, err := redpanda.Chart.Dot(nil, helmette.Release{
		Name:      "redpanda",
		Namespace: "redpanda",
		Service:   "Helm",
	}, struct{}{})
	require.NoError(t, err)

	state := &redpanda.RenderState{Dot: dot}
	values := redpanda.Values{}
	spec := &redpandav1alpha2.Statefulset{
		ExtraVolumeMounts: ptr.To("- name: io-config\n  mountPath: /etc/redpanda-io-config"),
		LivenessProbe: &redpandav1alpha2.LivenessProbe{
			InitialDelaySeconds: ptr.To(33),
		},
		StartupProbe: &redpandav1alpha2.StartupProbe{
			InitialDelaySeconds: ptr.To(44),
		},
	}

	require.NoError(t, convertStatefulsetV2Fields(state, &values, spec))

	containerByName := func(name string) *applycorev1.ContainerApplyConfiguration {
		for i, container := range values.Statefulset.PodTemplate.Spec.Containers {
			if ptr.Deref(container.Name, "") == name {
				return &values.Statefulset.PodTemplate.Spec.Containers[i]
			}
		}
		t.Fatalf("container %q not found", name)
		return nil
	}
	mountNames := func(container *applycorev1.ContainerApplyConfiguration) []string {
		return functional.MapFn(func(m applycorev1.VolumeMountApplyConfiguration) string {
			return ptr.Deref(m.Name, "")
		}, container.VolumeMounts)
	}

	broker := containerByName(redpanda.RedpandaContainerName)
	require.Contains(t, mountNames(broker), "io-config", "extraVolumeMounts must land on the broker container")
	require.NotNil(t, broker.LivenessProbe)
	require.Equal(t, ptr.To(int32(33)), broker.LivenessProbe.InitialDelaySeconds, "livenessProbe must land on the broker container")
	require.NotNil(t, broker.StartupProbe)
	require.Equal(t, ptr.To(int32(44)), broker.StartupProbe.InitialDelaySeconds, "startupProbe must land on the broker container")

	sidecar := containerByName(redpanda.SidecarContainerName)
	require.Contains(t, mountNames(sidecar), "io-config", "extraVolumeMounts must also land on the sidecar container")
}

// TestDuplicateBrokerContainerOverridesSurviveRender guards the agreement
// between the conversion helpers and the chart's pod template merge when a CR
// carries duplicate entries for the same container name (the CRD schema is a
// plain array, so duplicates pass admission). The chart's mergeSliceBy keeps
// only the last duplicate, so a conversion write to any earlier duplicate
// (probes, extraVolumeMounts) would land on an entry the chart never renders
// — the same silent-drop failure as issue #1577, for malformed-but-accepted
// input. containerOrInit therefore has to match the LAST duplicate.
func TestDuplicateBrokerContainerOverridesSurviveRender(t *testing.T) {
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp",
			Namespace: "redpanda",
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					ExtraVolumeMounts: ptr.To("- name: io-config\n  mountPath: /etc/redpanda-io-config"),
					LivenessProbe: &redpandav1alpha2.LivenessProbe{
						InitialDelaySeconds: ptr.To(33),
					},
					StartupProbe: &redpandav1alpha2.StartupProbe{
						InitialDelaySeconds: ptr.To(44),
					},
					PodTemplate: &redpandav1alpha2.PodTemplate{
						Spec: &applycorev1.PodSpecApplyConfiguration{
							Containers: []applycorev1.ContainerApplyConfiguration{
								{
									Name: ptr.To(redpanda.RedpandaContainerName),
									Env: []applycorev1.EnvVarApplyConfiguration{
										{Name: ptr.To("FROM_FIRST_DUPLICATE"), Value: ptr.To("1")},
									},
								},
								{
									Name: ptr.To(redpanda.RedpandaContainerName),
									Env: []applycorev1.EnvVarApplyConfiguration{
										{Name: ptr.To("FROM_LAST_DUPLICATE"), Value: ptr.To("1")},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	state, err := ConvertV2ToRenderState(nil, &V2Defaulters{}, cluster, nil)
	require.NoError(t, err)

	sets := redpanda.StatefulSets(state)
	require.NotEmpty(t, sets)

	var broker *corev1.Container
	for i, container := range sets[0].Spec.Template.Spec.Containers {
		if container.Name == redpanda.RedpandaContainerName {
			require.Nil(t, broker, "rendered pod spec must contain exactly one redpanda container")
			broker = &sets[0].Spec.Template.Spec.Containers[i]
		}
	}
	require.NotNil(t, broker, "redpanda container not found in rendered StatefulSet")

	require.NotNil(t, broker.LivenessProbe)
	require.Equal(t, int32(33), broker.LivenessProbe.InitialDelaySeconds, "livenessProbe must survive duplicate container overrides")
	require.NotNil(t, broker.StartupProbe)
	require.Equal(t, int32(44), broker.StartupProbe.InitialDelaySeconds, "startupProbe must survive duplicate container overrides")

	mountNames := functional.MapFn(func(m corev1.VolumeMount) string { return m.Name }, broker.VolumeMounts)
	require.Contains(t, mountNames, "io-config", "extraVolumeMounts must survive duplicate container overrides")

	envNames := functional.MapFn(func(e corev1.EnvVar) string { return e.Name }, broker.Env)
	require.Contains(t, envNames, "FROM_LAST_DUPLICATE", "the last duplicate wins, matching the chart's merge semantics")
	require.NotContains(t, envNames, "FROM_FIRST_DUPLICATE", "earlier duplicates are dropped wholesale, matching the chart's merge semantics")
}

// TestDuplicateInitContainerOverridesSurviveRender covers the second entry
// vector for duplicate container names: extraInitContainers is appended to
// the pod template's initContainers during conversion, so an entry there can
// duplicate one declared in podTemplate.spec.initContainers. Conversion
// writes (e.g. the configurator's extraVolumeMounts) must land on the entry
// the chart's last-wins merge actually renders.
func TestDuplicateInitContainerOverridesSurviveRender(t *testing.T) {
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp",
			Namespace: "redpanda",
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Statefulset: &redpandav1alpha2.Statefulset{
					InitContainers: &redpandav1alpha2.InitContainers{
						Configurator: &redpandav1alpha2.Configurator{
							ExtraVolumeMounts: ptr.To("- name: cfg-extra\n  mountPath: /cfg-extra"),
						},
						ExtraInitContainers: ptr.To("- name: " + redpanda.RedpandaConfiguratorContainerName + "\n  env:\n  - name: FROM_LAST_DUPLICATE\n    value: \"1\""),
					},
					PodTemplate: &redpandav1alpha2.PodTemplate{
						Spec: &applycorev1.PodSpecApplyConfiguration{
							InitContainers: []applycorev1.ContainerApplyConfiguration{
								{
									Name: ptr.To(redpanda.RedpandaConfiguratorContainerName),
									Env: []applycorev1.EnvVarApplyConfiguration{
										{Name: ptr.To("FROM_FIRST_DUPLICATE"), Value: ptr.To("1")},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	state, err := ConvertV2ToRenderState(nil, &V2Defaulters{}, cluster, nil)
	require.NoError(t, err)

	sets := redpanda.StatefulSets(state)
	require.NotEmpty(t, sets)

	var configurator *corev1.Container
	for i, container := range sets[0].Spec.Template.Spec.InitContainers {
		if container.Name == redpanda.RedpandaConfiguratorContainerName {
			require.Nil(t, configurator, "rendered pod spec must contain exactly one configurator init container")
			configurator = &sets[0].Spec.Template.Spec.InitContainers[i]
		}
	}
	require.NotNil(t, configurator, "configurator init container not found in rendered StatefulSet")

	mountNames := functional.MapFn(func(m corev1.VolumeMount) string { return m.Name }, configurator.VolumeMounts)
	require.Contains(t, mountNames, "cfg-extra", "configurator extraVolumeMounts must survive duplicate init container overrides")

	envNames := functional.MapFn(func(e corev1.EnvVar) string { return e.Name }, configurator.Env)
	require.Contains(t, envNames, "FROM_LAST_DUPLICATE", "the last duplicate wins, matching the chart's merge semantics")
	require.NotContains(t, envNames, "FROM_FIRST_DUPLICATE", "earlier duplicates are dropped wholesale, matching the chart's merge semantics")
}

func TestConvertV2Fields(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		partialValues := rapid.MakeCustom[redpanda.PartialValues](fuzzing.ClusterSpecConfig()).Draw(t, "values")
		if partialValues.Storage != nil && partialValues.Storage.Tiered != nil && partialValues.Storage.Tiered.PersistentVolume != nil {
			partialValues.Storage.Tiered.PersistentVolume.Size = nil
		}
		values := redpanda.Values{}
		clusterSpec := &redpandav1alpha2.RedpandaClusterSpec{
			Affinity: &corev1.Affinity{},
			Statefulset: &redpandav1alpha2.Statefulset{
				PodAffinity: &corev1.PodAffinity{},
			},
		}
		marshaled, err := json.Marshal(partialValues)
		require.NoError(t, err)

		require.NoError(t, json.Unmarshal(marshaled, clusterSpec))
		require.NoError(t, json.Unmarshal(marshaled, &values))
		state := &redpanda.RenderState{
			Values: values,
		}
		err = convertV2Fields(state, &state.Values, clusterSpec)
		require.NoError(t, err)
	})
}
