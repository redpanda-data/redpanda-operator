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
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// TestNodePoolImagePrecedence pins the per-field precedence for a NodePool's
// broker image: NodePool > Redpanda CR > operator default. Skipping the
// Redpanda CR level splits a cluster with a pinned tag across versions
// (https://redpandadata.atlassian.net/browse/K8S-957).
func TestNodePoolImagePrecedence(t *testing.T) {
	operatorDefault := func(image *redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage {
		if image == nil {
			image = &redpandav1alpha2.RedpandaImage{}
		}
		return &redpandav1alpha2.RedpandaImage{
			Repository: ptr.To(ptr.Deref(image.Repository, "operator/redpanda")),
			Tag:        ptr.To(ptr.Deref(image.Tag, "v99.0.0")),
		}
	}

	cases := []struct {
		name         string
		clusterImage *redpandav1alpha2.RedpandaImage
		poolImage    *redpandav1alpha2.RedpandaImage
		want         string
	}{
		{
			name: "neither set -> operator default",
			want: "operator/redpanda:v99.0.0",
		},
		{
			name:         "cluster tag only -> pool inherits cluster tag",
			clusterImage: &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v25.1.1")},
			want:         "operator/redpanda:v25.1.1",
		},
		{
			name:         "cluster only -> pool inherits cluster",
			clusterImage: &redpandav1alpha2.RedpandaImage{Repository: ptr.To("cluster/redpanda"), Tag: ptr.To("v25.1.1")},
			want:         "cluster/redpanda:v25.1.1",
		},
		{
			name:         "pool tag only -> cluster repository",
			clusterImage: &redpandav1alpha2.RedpandaImage{Repository: ptr.To("cluster/redpanda"), Tag: ptr.To("v25.1.1")},
			poolImage:    &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v25.2.1")},
			want:         "cluster/redpanda:v25.2.1",
		},
		{
			name:         "pool repository only -> cluster tag",
			clusterImage: &redpandav1alpha2.RedpandaImage{Tag: ptr.To("v25.1.1")},
			poolImage:    &redpandav1alpha2.RedpandaImage{Repository: ptr.To("pool/redpanda")},
			want:         "pool/redpanda:v25.1.1",
		},
		{
			name:         "pool overrides cluster",
			clusterImage: &redpandav1alpha2.RedpandaImage{Repository: ptr.To("cluster/redpanda"), Tag: ptr.To("v25.1.1")},
			poolImage:    &redpandav1alpha2.RedpandaImage{Repository: ptr.To("pool/redpanda"), Tag: ptr.To("v25.2.1")},
			want:         "pool/redpanda:v25.2.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &redpandav1alpha2.Redpanda{
				ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "redpanda"},
				Spec: redpandav1alpha2.RedpandaSpec{
					ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{Image: tc.clusterImage},
				},
			}
			pool := &redpandav1alpha2.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "redpanda"},
				Spec: redpandav1alpha2.NodePoolSpec{
					EmbeddedNodePoolSpec: redpandav1alpha2.EmbeddedNodePoolSpec{Image: tc.poolImage},
				},
			}

			state, err := ConvertV2ToRenderState(nil, &V2Defaulters{RedpandaImage: operatorDefault}, cluster, []*redpandav1alpha2.NodePool{pool})
			require.NoError(t, err)

			sets, err := redpanda.RenderNodePools(state)
			require.NoError(t, err)

			var poolSet *appsv1.StatefulSet
			for _, set := range sets {
				if set.Labels[redpanda.NodePoolLabelName] == pool.Name {
					poolSet = set
				}
			}
			require.NotNil(t, poolSet, "pool StatefulSet not rendered")

			images := map[string]string{}
			for _, container := range append(poolSet.Spec.Template.Spec.InitContainers, poolSet.Spec.Template.Spec.Containers...) {
				images[container.Name] = container.Image
			}
			require.Equal(t, tc.want, images[redpanda.RedpandaContainerName])
			require.Equal(t, tc.want, images[redpanda.RedpandaConfiguratorContainerName])
		})
	}
}

// TestPodTemplateStableAcrossChartVersions guards against rolling every broker
// on an operator upgrade: the operator embeds the redpanda chart, so its
// version must not reach the pod template, while a sidecar image change still
// must.
func TestPodTemplateStableAcrossChartVersions(t *testing.T) {
	render := func(t *testing.T, chartVersion, sidecarTag string) map[string]corev1.PodTemplateSpec {
		sets := renderOperatorPools(t, sidecarTag, redpandav1alpha2.EmbeddedNodePoolSpec{}, func(state *redpandachart.RenderState) {
			state.Chart.Version = chartVersion
		})

		templates := map[string]corev1.PodTemplateSpec{}
		for name, set := range sets {
			templates[name] = set.Spec.Template
		}
		return templates
	}

	baseline := render(t, "26.1.1", "v26.1.1")

	cases := []struct {
		name         string
		chartVersion string
		sidecarTag   string
		wantRoll     bool
	}{
		{name: "chart version bump", chartVersion: "26.2.1", sidecarTag: "v26.1.1", wantRoll: false},
		{name: "sidecar image change", chartVersion: "26.1.1", sidecarTag: "v26.2.1", wantRoll: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upgraded := render(t, tc.chartVersion, tc.sidecarTag)
			for name, template := range baseline {
				if tc.wantRoll {
					require.NotEqual(t, template, upgraded[name], "%s pod template", name)
				} else {
					require.Equal(t, template, upgraded[name], "%s pod template", name)
				}
			}
		})
	}
}

// TestPodTemplateChartLabel pins where the operator strips helm.sh/chart from
// pod templates: every pool, including the default pool's labels cached from
// the live StatefulSet, unless the StatefulSet's selector requires it.
func TestPodTemplateChartLabel(t *testing.T) {
	selector := map[string]string{
		"app.kubernetes.io/component": "redpanda-statefulset",
		"app.kubernetes.io/instance":  "rp",
		"app.kubernetes.io/name":      "redpanda",
	}
	withChartLabel := func(labels map[string]string) map[string]string {
		labels = maps.Clone(labels)
		labels["helm.sh/chart"] = "redpanda-5.9.0"
		return labels
	}

	cases := []struct {
		name           string
		set            string
		cachedLabels   map[string]string
		cachedSelector map[string]string
		poolSelector   map[string]string
		wantChartLabel bool
	}{
		{name: "new default pool", set: "rp"},
		{name: "named pool", set: "rp-pool"},
		{name: "cached default pool", set: "rp", cachedLabels: withChartLabel(selector), cachedSelector: selector},
		{name: "cached selector requires label", set: "rp", cachedLabels: withChartLabel(selector), cachedSelector: withChartLabel(selector), wantChartLabel: true},
		{name: "pool selector requires label", set: "rp-pool", poolSelector: map[string]string{"helm.sh/chart": "pinned"}, wantChartLabel: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sets := renderOperatorPools(t, "v26.1.1", redpandav1alpha2.EmbeddedNodePoolSpec{
				AdditionalSelectorLabels: tc.poolSelector,
			}, func(state *redpandachart.RenderState) {
				// What FetchStatefulSetPodSelector reads from a live StatefulSet.
				state.StatefulSetPodLabels = tc.cachedLabels
				state.StatefulSetSelector = tc.cachedSelector
			})

			require.Contains(t, sets, tc.set)
			_, ok := sets[tc.set].Spec.Template.Labels["helm.sh/chart"]
			require.Equal(t, tc.wantChartLabel, ok, "%s pod template labels: %v", tc.set, sets[tc.set].Spec.Template.Labels)
		})
	}
}

// renderOperatorPools renders the default pool and one NodePool, keyed by
// StatefulSet name, through the operator's conversion path with images
// pinned. mutate runs on the RenderState before rendering.
func renderOperatorPools(t *testing.T, sidecarTag string, poolSpec redpandav1alpha2.EmbeddedNodePoolSpec, mutate func(*redpandachart.RenderState)) map[string]*appsv1.StatefulSet {
	cluster := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "redpanda"},
	}
	pool := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "redpanda"},
		Spec:       redpandav1alpha2.NodePoolSpec{EmbeddedNodePoolSpec: poolSpec},
	}
	pin := func(repository, tag string) func(*redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage {
		return func(*redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage {
			return &redpandav1alpha2.RedpandaImage{Repository: ptr.To(repository), Tag: ptr.To(tag)}
		}
	}

	state, err := ConvertV2ToRenderState(nil, &V2Defaulters{
		RedpandaImage: pin("redpandadata/redpanda", "v26.1.1"),
		SidecarImage:  pin("redpandadata/redpanda-operator", sidecarTag),
	}, cluster, []*redpandav1alpha2.NodePool{pool})
	require.NoError(t, err)

	// REDPANDA_METRICS_K8S_CHART_VERSION tracks the operator version and
	// falls back to the chart version; pin it to isolate the chart.
	state.OperatorVersion = "v26.1.1"
	mutate(state)

	sets, err := redpandachart.RenderNodePools(state)
	require.NoError(t, err)
	require.Len(t, sets, 2)

	byName := map[string]*appsv1.StatefulSet{}
	for _, set := range sets {
		requireSelectorsMatch(t, set)
		byName[set.Name] = set
	}
	return byName
}

// requireSelectorsMatch asserts that both the StatefulSet's selector and the
// sidecar's --selector, which scopes its PVC unbinder to broker pods, select
// the pods set creates.
func requireSelectorsMatch(t *testing.T, set *appsv1.StatefulSet) {
	podLabels := labels.Set(set.Spec.Template.Labels)

	selector, err := metav1.LabelSelectorAsSelector(set.Spec.Selector)
	require.NoError(t, err)
	require.True(t, selector.Matches(podLabels), "%s: selector %v does not match pod labels %v", set.Name, set.Spec.Selector, podLabels)

	for _, container := range set.Spec.Template.Spec.Containers {
		if container.Name != redpandachart.SidecarContainerName {
			continue
		}
		for _, arg := range container.Args {
			raw, ok := strings.CutPrefix(arg, "--selector=")
			if !ok {
				continue
			}
			selector, err := labels.Parse(raw)
			require.NoError(t, err)
			require.True(t, selector.Matches(podLabels), "%s: sidecar selector %q does not match pod labels %v", set.Name, raw, podLabels)
			return
		}
	}
	t.Fatalf("%s: sidecar --selector not found", set.Name)
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
