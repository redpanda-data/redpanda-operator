// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package podtemplate provides strategic merge patch utilities for Kubernetes
// PodTemplateSpec objects.
package podtemplate

import (
	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// Overrides represents a set of PodTemplate overrides using Kubernetes
// apply-configuration types. This matches the shape of redpandav1alpha2.PodTemplate
// but avoids coupling this package to the CRD API types.
type Overrides struct {
	Labels      map[string]string                      `json:"labels,omitempty"`
	Annotations map[string]string                      `json:"annotations,omitempty"`
	Spec        *applycorev1.PodSpecApplyConfiguration `json:"spec,omitempty"`
}

// StrategicMergePatch applies PodTemplate overrides to an existing PodTemplateSpec
// using a strategic-merge-patch-like approach. Lists (containers, volumes, env vars,
// volume mounts) are merged by name key rather than replaced wholesale.
func StrategicMergePatch(overrides Overrides, original corev1.PodTemplateSpec) (corev1.PodTemplateSpec, error) {
	var zero corev1.PodTemplateSpec

	// Deep clone overrides via JSON round-trip to avoid mutability issues.
	cloned := tplutil.FromJSON(tplutil.ToJSON(overrides))
	var err error
	overrides, err = tplutil.MergeTo[Overrides](cloned)
	if err != nil {
		return zero, err
	}

	overrideSpec := overrides.Spec
	if overrideSpec == nil {
		overrideSpec = &applycorev1.PodSpecApplyConfiguration{}
	}

	merged, err := tplutil.MergeTo[corev1.PodTemplateSpec](
		applycorev1.PodTemplateSpecApplyConfiguration{
			ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
				Labels:      overrides.Labels,
				Annotations: overrides.Annotations,
			},
			Spec: overrideSpec,
		},
		original,
	)
	if err != nil {
		return zero, err
	}

	merged.Spec.InitContainers = tplutil.MergeSliceBy(
		original.Spec.InitContainers,
		overrideSpec.InitContainers,
		"name",
		mergeContainer,
	)

	merged.Spec.Containers = tplutil.MergeSliceBy(
		original.Spec.Containers,
		overrideSpec.Containers,
		"name",
		mergeContainer,
	)

	merged.Spec.Volumes = tplutil.MergeSliceBy(
		original.Spec.Volumes,
		overrideSpec.Volumes,
		"name",
		mergeVolume,
	)

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

	return merged, nil
}

func mergeEnvVar(_ corev1.EnvVar, overrides applycorev1.EnvVarApplyConfiguration) corev1.EnvVar {
	result, _ := tplutil.MergeTo[corev1.EnvVar](overrides)
	return result
}

func mergeVolume(_ corev1.Volume, override applycorev1.VolumeApplyConfiguration) corev1.Volume {
	result, _ := tplutil.MergeTo[corev1.Volume](override)
	return result
}

func mergeVolumeMount(original corev1.VolumeMount, override applycorev1.VolumeMountApplyConfiguration) corev1.VolumeMount {
	result, _ := tplutil.MergeTo[corev1.VolumeMount](override, original)
	return result
}

func mergeContainer(original corev1.Container, override applycorev1.ContainerApplyConfiguration) corev1.Container {
	merged, _ := tplutil.MergeTo[corev1.Container](override, original)
	merged.Env = tplutil.MergeSliceBy(original.Env, override.Env, "name", mergeEnvVar)
	merged.VolumeMounts = tplutil.MergeSliceBy(original.VolumeMounts, override.VolumeMounts, "name", mergeVolumeMount)
	return merged
}
