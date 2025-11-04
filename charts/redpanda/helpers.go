// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_helpers.go.tpl
package redpanda

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	//nolint:stylecheck
	redpanda_22_2_0 = ">=22.2.0-0 || <0.0.1-0"
	//nolint:stylecheck
	redpanda_22_3_0 = ">=22.3.0-0 || <0.0.1-0"
	//nolint:stylecheck
	redpanda_23_1_1 = ">=23.1.1-0 || <0.0.1-0"
	//nolint:stylecheck
	redpanda_23_1_2 = ">=23.1.2-0 || <0.0.1-0"
	//nolint:stylecheck
	redpanda_22_3_atleast_22_3_13 = ">=22.3.13-0,<22.4"
	//nolint:stylecheck
	redpanda_22_2_atleast_22_2_10 = ">=22.2.10-0,<22.3"
	//nolint:stylecheck
	redpanda_23_2_1 = ">=23.2.1-0 || <0.0.1-0"
	//nolint:stylecheck
	redpanda_23_3_0 = ">=23.3.0-0 || <0.0.1-0"
)

// Create chart name and version as used by the chart label.
func ChartLabel(state *RenderState) string {
	return cleanForK8s(strings.ReplaceAll(fmt.Sprintf("%s-%s", state.Chart.Name, state.Chart.Version), "+", "_"))
}

// Name returns the name of this chart as specified in Chart.yaml, unless
// explicitly overridden.
// Name is effectively static and should not be used for naming of resources.
// Name is truncated at 63 characters to satisfy Kubernetes field limits
// and DNS limits.
func Name(state *RenderState) string {
	if override := state.Values.NameOverride; override != "" {
		return cleanForK8s(override)
	}
	return cleanForK8s(state.Chart.Name)
}

// Fullname returns the name of this helm release, unless explicitly
// overridden.
// Fullname is truncated at 63 characters to satisfy Kubernetes field limits
// and DNS limits.
func Fullname(state *RenderState) string {
	if override := state.Values.FullnameOverride; override != "" {
		return cleanForK8s(override)
	}
	return cleanForK8s(state.Release.Name)
}

// full helm labels + common labels
func FullLabels(state *RenderState) map[string]string {
	labels := map[string]string{}
	if state.Values.CommonLabels != nil {
		labels = state.Values.CommonLabels
	}

	defaults := map[string]string{
		"helm.sh/chart":                ChartLabel(state),
		"app.kubernetes.io/name":       Name(state),
		"app.kubernetes.io/instance":   state.Release.Name,
		"app.kubernetes.io/managed-by": state.Release.Service,
		"app.kubernetes.io/component":  Name(state),
	}

	return helmette.Merge(labels, defaults)
}

// Use AppVersion if image.tag is not set
func Tag(state *RenderState) string {
	tag := string(state.Values.Image.Tag)
	if tag == "" {
		tag = state.Chart.AppVersion
	}

	// pattern := "^v(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$"

	//if !helmette.RegexMatch(pattern, tag) {
	//	// This error message is for end users. This can also occur if
	//	// AppVersion doesn't start with a 'v' in Chart.yaml.
	//	panic("image.tag must start with a 'v' and be a valid semver")
	//}

	return tag
}

// Create a default service name
func ServiceName(state *RenderState) string {
	if state.Values.Service != nil && state.Values.Service.Name != nil {
		return cleanForK8s(*state.Values.Service.Name)
	}

	return Fullname(state)
}

// Generate internal fqdn
func InternalDomain(state *RenderState) string {
	service := ServiceName(state)
	ns := state.Release.Namespace

	return fmt.Sprintf("%s.%s.svc.%s", service, ns, state.Values.ClusterDomain)
}

// mounts that are common to most containers
func DefaultMounts(state *RenderState) []corev1.VolumeMount {
	return append([]corev1.VolumeMount{
		{
			Name:      "base-config",
			MountPath: "/etc/redpanda",
		},
	}, CommonMounts(state)...)
}

// mounts that are common to all containers
func CommonMounts(state *RenderState) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{}

	if sasl := state.Values.Auth.SASL; sasl.Enabled && sasl.SecretRef != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "users",
			MountPath: "/etc/secrets/users",
			ReadOnly:  true,
		})
	}

	for _, name := range state.Values.Listeners.InUseServerCerts(&state.Values.TLS) {
		cert := state.Values.TLS.Certs.MustGet(name)

		mounts = append(mounts, corev1.VolumeMount{
			Name:      cert.ServerVolumeName(name),
			MountPath: cert.ServerMountPoint(name),
		})
	}

	// mTLS for any potentially in use listeners (kafka, admin, schema?)
	for _, name := range state.Values.Listeners.InUseClientCerts(&state.Values.TLS) {
		cert := state.Values.TLS.Certs.MustGet(name)

		mounts = append(mounts, corev1.VolumeMount{
			Name:      cert.ClientVolumeName(name),
			MountPath: cert.ClientMountPoint(name),
		})

	}

	return mounts
}

func DefaultVolumes(state *RenderState) []corev1.Volume {
	return append([]corev1.Volume{
		{
			Name: "base-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: Fullname(state),
					},
				},
			},
		},
	}, CommonVolumes(state)...)
}

// volumes that are common to all pods
func CommonVolumes(state *RenderState) []corev1.Volume {
	volumes := []corev1.Volume{}

	for _, name := range state.Values.Listeners.InUseServerCerts(&state.Values.TLS) {
		cert := state.Values.TLS.Certs.MustGet(name)

		volumes = append(volumes, corev1.Volume{
			// Intentionally use static names for VolumeNames to make overrides easier.
			Name: cert.ServerVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  cert.ServerSecretName(state, name),
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		})
	}

	for _, name := range state.Values.Listeners.InUseClientCerts(&state.Values.TLS) {
		cert := state.Values.TLS.Certs.MustGet(name)

		volumes = append(volumes, corev1.Volume{
			Name: cert.ClientVolumeName(name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  cert.ClientSecretName(state, name),
					DefaultMode: ptr.To[int32](0o440),
				},
			},
		})
	}

	if sasl := state.Values.Auth.SASL; sasl.Enabled && sasl.SecretRef != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "users",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sasl.SecretRef,
				},
			},
		})
	}

	return volumes
}

//nolint:stylecheck
func RedpandaAtLeast_22_2_0(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_22_2_0)
}

//nolint:stylecheck
func RedpandaAtLeast_22_3_0(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_22_3_0)
}

//nolint:stylecheck
func RedpandaAtLeast_23_1_1(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_23_1_1)
}

//nolint:stylecheck
func RedpandaAtLeast_23_1_2(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_23_1_2)
}

//nolint:stylecheck
func RedpandaAtLeast_22_3_atleast_22_3_13(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_22_3_atleast_22_3_13)
}

//nolint:stylecheck
func RedpandaAtLeast_22_2_atleast_22_2_10(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_22_2_atleast_22_2_10)
}

//nolint:stylecheck
func RedpandaAtLeast_23_2_1(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_23_2_1)
}

//nolint:stylecheck
func RedpandaAtLeast_23_3_0(state *RenderState) bool {
	return redpandaAtLeast(state, redpanda_23_3_0)
}

func redpandaAtLeast(state *RenderState, constraint string) bool {
	version := strings.TrimPrefix(Tag(state), "v")

	result, err := helmette.SemverCompare(constraint, version)
	if err != nil {
		panic(err)
	}
	return result
}

func cleanForK8s(in string) string {
	return strings.TrimSuffix(helmette.Trunc(63, in), "-")
}

// StructuredTpl (inefficiently) recurses through all fields of T and expands
// any string fields containing template delimiters with [helmette.Tpl].
func StructuredTpl[T any](state *RenderState, in T) T {
	untyped := helmette.UnmarshalInto[map[string]any](in)
	expanded := recursiveTpl(state, untyped)
	return helmette.MergeTo[T](expanded)
}

// recursiveTpl is a helper for [StructuredTpl]. It performs all the works, it
// just operates on untyped values.
func recursiveTpl(state *RenderState, data any) any {
	kind := helmette.KindOf(data)

	if kind == "map" {
		m := data.(map[string]any)
		for key, value := range m {
			m[key] = recursiveTpl(state, value)
		}
		return m
	} else if kind == "slice" {
		// NB: Slices in helm are immutable so we have to make a new slice here.
		s := data.([]any)
		var out []any
		for i := range s {
			out = append(out, recursiveTpl(state, s[i]))
		}
		return out
	} else if kind == "string" && helmette.Contains("{{", data.(string)) {
		// Tpl is quite slow, so we gate this on template delimiters for a
		// little speed up.
		return helmette.Tpl(state.Dot, data.(string), state.Dot)
	}

	return data
}

// StrategicMergePatch is a half-baked implementation of Kubernetes' strategic
// merge patch. It's closer to a merge patch with smart handling of lists
// that's tailored to the values permitted by [PodTemplate].
func StrategicMergePatch(overrides PodTemplate, original corev1.PodTemplateSpec) corev1.PodTemplateSpec {
	// Divergences from an actual SMP:
	// - No support for Directives
	// - List merging by key is handled on a case by case basis.
	// - Can't "unset" optional values in the original due to there being no
	//   difference between *T being explicitly nil or not set.

	// Nasty hack to work around mutability issues when using MergeTo on a
	// deeply nested object. gotohelm doesn't currently have a deepCopy method,
	// so we marshal to JSON and then unmarshal back into the same type.
	overridesClone := helmette.FromJSON(helmette.ToJSON(overrides))
	overrides = helmette.MergeTo[PodTemplate](overridesClone)

	overrideSpec := overrides.Spec
	if overrideSpec == nil {
		overrideSpec = &applycorev1.PodSpecApplyConfiguration{}
	}

	merged := helmette.MergeTo[corev1.PodTemplateSpec](
		applycorev1.PodTemplateSpecApplyConfiguration{
			ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
				Labels:      overrides.Labels,
				Annotations: overrides.Annotations,
			},
			Spec: overrideSpec,
		},
		original,
	)

	merged.Spec.InitContainers = mergeSliceBy(
		original.Spec.InitContainers,
		overrideSpec.InitContainers,
		"name",
		mergeContainer,
	)

	merged.Spec.Containers = mergeSliceBy(
		original.Spec.Containers,
		overrideSpec.Containers,
		"name",
		mergeContainer,
	)

	merged.Spec.Volumes = mergeSliceBy(
		original.Spec.Volumes,
		overrideSpec.Volumes,
		"name",
		mergeVolume,
	)

	// Due to quirks in go's JSON marshalling and some default values in the
	// chart, GoHelmEquivalence can fail with meaningless diffs of null vs
	// empty slice/map. This defaulting ensures we are in fact equivalent at
	// all times but a functionally not required.
	if merged.ObjectMeta.Labels == nil {
		merged.ObjectMeta.Labels = map[string]string{}
	}

	if merged.ObjectMeta.Annotations == nil {
		merged.ObjectMeta.Annotations = map[string]string{}
	}

	if merged.Spec.NodeSelector == nil {
		merged.Spec.NodeSelector = map[string]string{}
	}

	if merged.Spec.Tolerations == nil {
		merged.Spec.Tolerations = []corev1.Toleration{}
	}

	if merged.Spec.ImagePullSecrets == nil {
		merged.Spec.ImagePullSecrets = []corev1.LocalObjectReference{}
	}

	return merged
}

func mergeSliceBy[Original any, Overrides any](
	original []Original,
	override []Overrides,
	mergeKey string,
	mergeFunc func(Original, Overrides) Original,
) []Original {
	originalKeys := map[string]bool{}
	overrideByKey := map[string]Overrides{}

	for _, el := range override {
		key, ok := helmette.Get[string](el, mergeKey)
		if !ok {
			continue
		}
		overrideByKey[key] = el
	}

	// Follow the ordering of original, merging in overrides as needed.
	var merged []Original
	for _, el := range original {
		// Cheating a bit here. We know that "original" types will always have
		// the key we're looking for.
		key, _ := helmette.Get[string](el, mergeKey)
		originalKeys[key] = true

		if elOverride, ok := overrideByKey[key]; ok {
			merged = append(merged, mergeFunc(el, elOverride))
		} else {
			merged = append(merged, el)
		}
	}

	// Append any non-merged overrides.
	for _, el := range override {
		key, ok := helmette.Get[string](el, mergeKey)
		if !ok {
			continue
		}

		if _, ok := originalKeys[key]; ok {
			continue
		}

		merged = append(merged, helmette.MergeTo[Original](el))
	}

	return merged
}

func mergeEnvVar(original corev1.EnvVar, overrides applycorev1.EnvVarApplyConfiguration) corev1.EnvVar {
	// If there's a case of having an env overridden, don't merge. Just accept
	// the override as merging could generate an env with multiple sources.
	return helmette.MergeTo[corev1.EnvVar](overrides)
}

func mergeVolume(original corev1.Volume, override applycorev1.VolumeApplyConfiguration) corev1.Volume {
	// Similar to the above, if a volume is being overridden, it's likely to
	// change the VolumeSource. Don't merge, just accept the override.
	return helmette.MergeTo[corev1.Volume](override)
}

func mergeVolumeMount(original corev1.VolumeMount, override applycorev1.VolumeMountApplyConfiguration) corev1.VolumeMount {
	return helmette.MergeTo[corev1.VolumeMount](override, original)
}

func mergeContainer(original corev1.Container, override applycorev1.ContainerApplyConfiguration) corev1.Container {
	merged := helmette.MergeTo[corev1.Container](override, original)
	merged.Env = mergeSliceBy(original.Env, override.Env, "name", mergeEnvVar)
	merged.VolumeMounts = mergeSliceBy(original.VolumeMounts, override.VolumeMounts, "name", mergeVolumeMount)
	return merged
}

func PodNames(state *RenderState, pool Pool) []string {
	suffix := ""
	if pool.Name != "" {
		suffix = fmt.Sprintf("-%s", pool.Name)
	}

	var pods []string
	for i := int32(0); i < pool.Statefulset.Replicas; i++ {
		pods = append(pods, fmt.Sprintf("%s%s-%d", Fullname(state), suffix, i))
	}

	return pods
}
