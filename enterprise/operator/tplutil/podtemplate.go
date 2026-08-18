// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// This file provides strategic merge patch utilities for Kubernetes
// PodTemplateSpec objects.

package tplutil

import (
	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// PodOverrides represents a set of PodTemplate overrides using Kubernetes
// apply-configuration types. This matches the shape of redpandav1alpha2.PodTemplate
// but avoids coupling this package to the CRD API types.
type PodOverrides struct {
	Labels      map[string]string                      `json:"labels,omitempty"`
	Annotations map[string]string                      `json:"annotations,omitempty"`
	Spec        *applycorev1.PodSpecApplyConfiguration `json:"spec,omitempty"`
}

// PodStrategicMergePatch applies PodTemplate overrides to an existing PodTemplateSpec
// using a strategic-merge-patch-like approach. Lists (containers, volumes, env vars,
// volume mounts) are merged by name key rather than replaced wholesale.
func PodStrategicMergePatch(overrides PodOverrides, original corev1.PodTemplateSpec) (corev1.PodTemplateSpec, error) {
	var zero corev1.PodTemplateSpec

	// Deep clone overrides via JSON round-trip to avoid mutability issues.
	cloned := FromJSON(ToJSON(overrides))
	var err error
	overrides, err = MergeTo[PodOverrides](cloned)
	if err != nil {
		return zero, err
	}

	overrideSpec := overrides.Spec
	if overrideSpec == nil {
		overrideSpec = &applycorev1.PodSpecApplyConfiguration{}
	}

	merged, err := MergeTo[corev1.PodTemplateSpec](
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

	merged.Spec.InitContainers = MergeSliceBy(
		original.Spec.InitContainers,
		overrideSpec.InitContainers,
		"name",
		mergeContainer,
	)

	merged.Spec.Containers = MergeSliceBy(
		original.Spec.Containers,
		overrideSpec.Containers,
		"name",
		mergeContainer,
	)

	merged.Spec.Volumes = MergeSliceBy(
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
	result, _ := MergeTo[corev1.EnvVar](overrides)
	return result
}

func mergeVolume(_ corev1.Volume, override applycorev1.VolumeApplyConfiguration) corev1.Volume {
	result, _ := MergeTo[corev1.Volume](override)
	return result
}

func mergeVolumeMount(original corev1.VolumeMount, override applycorev1.VolumeMountApplyConfiguration) corev1.VolumeMount {
	result, _ := MergeTo[corev1.VolumeMount](override, original)
	return result
}

func mergeContainer(original corev1.Container, override applycorev1.ContainerApplyConfiguration) corev1.Container {
	merged, _ := MergeTo[corev1.Container](override, original)
	merged.Env = MergeSliceBy(original.Env, override.Env, "name", mergeEnvVar)
	merged.VolumeMounts = MergeSliceBy(original.VolumeMounts, override.VolumeMounts, "name", mergeVolumeMount)
	return merged
}
