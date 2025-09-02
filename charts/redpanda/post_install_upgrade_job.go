// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_post-install-upgrade-job.go.tpl
package redpanda

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// bootstrapYamlTemplater returns an initcontainer that will template
// environment variables into ${base-config}/boostrap.yaml and output it to
// ${config}/.bootstrap.yaml.
func bootstrapYamlTemplater(state *RenderState, ss Statefulset) corev1.Container {
	env := state.Values.Storage.Tiered.CredentialsSecretRef.AsEnvVars(state.Values.Storage.GetTieredStorageConfig())
	_, _, additionalEnv := state.Values.Config.ExtraClusterConfiguration.Translate()
	env = append(env, additionalEnv...)

	image := fmt.Sprintf(`%s:%s`,
		ss.SideCars.Image.Repository,
		ss.SideCars.Image.Tag,
	)

	return corev1.Container{
		Name:  "bootstrap-yaml-envsubst",
		Image: image,
		Command: append([]string{
			"/redpanda-operator",
			"bootstrap",
			"--in-dir",
			"/tmp/base-config",
			"--out-dir",
			"/tmp/config",
		}, state.Values.Statefulset.InitContainers.Configurator.AdditionalCLIArgs...),
		Env: env,
		Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("125Mi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("125Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			// NB: RunAsUser and RunAsGroup will be inherited from the
			// PodSecurityContext of consumers.
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			RunAsNonRoot:             ptr.To(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: "/tmp/config/"},
			{Name: "base-config", MountPath: "/tmp/base-config/"},
		},
	}
}

func PostInstallUpgradeJob(state *RenderState) *batchv1.Job {
	if !state.Values.PostInstallJob.Enabled {
		return nil
	}

	// NB: since the job is a singleton, we only use the sidecar
	// image from the default statefulset for rendering
	image := fmt.Sprintf(`%s:%s`,
		state.Values.Statefulset.SideCars.Image.Repository,
		state.Values.Statefulset.SideCars.Image.Tag,
	)

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-configuration", Fullname(state)),
			Namespace: state.Release.Namespace,
			Labels: helmette.Merge(
				helmette.Default(map[string]string{}, state.Values.PostInstallJob.Labels),
				FullLabels(state),
			),
			Annotations: helmette.Merge(
				helmette.Default(map[string]string{}, state.Values.PostInstallJob.Annotations),
				// This is what defines this resource as a hook. Without this line, the
				// job is considered part of the release.
				map[string]string{
					"helm.sh/hook":               "post-install,post-upgrade",
					"helm.sh/hook-delete-policy": "before-hook-creation",
					"helm.sh/hook-weight":        "-5",
				},
			),
		},
		Spec: batchv1.JobSpec{
			Template: StrategicMergePatch(
				StructuredTpl(state, state.Values.PostInstallJob.PodTemplate),
				StrategicMergePatch(
					StructuredTpl(state, state.Values.PodTemplate),
					corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: fmt.Sprintf("%s-post-", state.Release.Name),
							Labels: helmette.Merge(
								map[string]string{
									"app.kubernetes.io/name":      Name(state),
									"app.kubernetes.io/instance":  state.Release.Name,
									"app.kubernetes.io/component": fmt.Sprintf("%.50s-post-install", Name(state)),
								},
								helmette.Default(map[string]string{}, state.Values.CommonLabels),
							),
						},
						Spec: corev1.PodSpec{
							RestartPolicy:                corev1.RestartPolicyNever,
							InitContainers:               []corev1.Container{bootstrapYamlTemplater(state, state.Values.Statefulset)},
							AutomountServiceAccountToken: ptr.To(false),
							Containers: []corev1.Container{
								{
									Name:  PostInstallContainerName,
									Image: image,
									Env:   PostInstallUpgradeEnvironmentVariables(state),
									// See sync-cluster-config in the operator for exact details. Roughly, it:
									// 1. Sets the redpanda license
									// 2. Sets the redpanda cluster config
									// 3. Restarts schema-registry (see https://github.com/redpanda-data/redpanda-operator/issues/232)
									// Upon the post-install run, the clusters's
									// configuration will be re-set (that is set again
									// not reset) which is an unfortunate but ultimately acceptable side effect.
									Command: []string{
										"/redpanda-operator",
										"sync-cluster-config",
										"--users-directory", "/etc/secrets/users",
										"--redpanda-yaml", "/tmp/base-config/redpanda.yaml",
										"--bootstrap-yaml", "/tmp/config/.bootstrap.yaml",
									},
									VolumeMounts: append(
										CommonMounts(state),
										corev1.VolumeMount{Name: "config", MountPath: "/tmp/config"},
										corev1.VolumeMount{Name: "base-config", MountPath: "/tmp/base-config"},
									),
								},
							},
							Volumes: append(
								CommonVolumes(state),
								corev1.Volume{
									Name: "base-config",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: Fullname(state),
											},
										},
									},
								},
								corev1.Volume{
									Name: "config",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							),
							ServiceAccountName: ServiceAccountName(state),
						},
					},
				),
			),
		},
	}

	return job
}

// PostInstallUpgradeEnvironmentVariables returns environment variables assigned to Redpanda
// container.
func PostInstallUpgradeEnvironmentVariables(state *RenderState) []corev1.EnvVar {
	envars := []corev1.EnvVar{}

	if license := state.Values.Enterprise.License; license != "" {
		envars = append(envars, corev1.EnvVar{
			Name:  "REDPANDA_LICENSE",
			Value: license,
		})
	} else if secretReference := state.Values.Enterprise.LicenseSecretRef; secretReference != nil {
		envars = append(envars, corev1.EnvVar{
			Name: "REDPANDA_LICENSE",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: secretReference,
			},
		})
	}

	// include any authentication envvars as well.
	return bootstrapEnvVars(state, envars)
}
