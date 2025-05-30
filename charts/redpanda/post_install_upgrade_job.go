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
func bootstrapYamlTemplater(dot *helmette.Dot) corev1.Container {
	values := helmette.Unwrap[Values](dot.Values)

	env := values.Storage.Tiered.CredentialsSecretRef.AsEnvVars(values.Storage.GetTieredStorageConfig())
	_, _, additionalEnv := values.Config.ExtraClusterConfiguration.Translate()
	env = append(env, additionalEnv...)

	image := fmt.Sprintf(`%s:%s`,
		values.Statefulset.SideCars.Image.Repository,
		values.Statefulset.SideCars.Image.Tag,
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
		}, values.Statefulset.InitContainers.Configurator.AdditionalCLIArgs...),
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

func PostInstallUpgradeJob(dot *helmette.Dot) *batchv1.Job {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.PostInstallJob.Enabled {
		return nil
	}

	image := fmt.Sprintf(`%s:%s`,
		values.Statefulset.SideCars.Image.Repository,
		values.Statefulset.SideCars.Image.Tag,
	)

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-configuration", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Labels: helmette.Merge(
				helmette.Default(map[string]string{}, values.PostInstallJob.Labels),
				FullLabels(dot),
			),
			Annotations: helmette.Merge(
				helmette.Default(map[string]string{}, values.PostInstallJob.Annotations),
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
				StructuredTpl(dot, values.PostInstallJob.PodTemplate),
				StrategicMergePatch(
					StructuredTpl(dot, values.PodTemplate),
					corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: fmt.Sprintf("%s-post-", dot.Release.Name),
							Labels: helmette.Merge(
								map[string]string{
									"app.kubernetes.io/name":      Name(dot),
									"app.kubernetes.io/instance":  dot.Release.Name,
									"app.kubernetes.io/component": fmt.Sprintf("%.50s-post-install", Name(dot)),
								},
								helmette.Default(map[string]string{}, values.CommonLabels),
							),
						},
						Spec: corev1.PodSpec{
							RestartPolicy:                corev1.RestartPolicyNever,
							InitContainers:               []corev1.Container{bootstrapYamlTemplater(dot)},
							AutomountServiceAccountToken: ptr.To(false),
							Containers: []corev1.Container{
								{
									Name:  PostInstallContainerName,
									Image: image,
									Env:   PostInstallUpgradeEnvironmentVariables(dot),
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
										CommonMounts(dot),
										corev1.VolumeMount{Name: "config", MountPath: "/tmp/config"},
										corev1.VolumeMount{Name: "base-config", MountPath: "/tmp/base-config"},
									),
								},
							},
							Volumes: append(
								CommonVolumes(dot),
								corev1.Volume{
									Name: "base-config",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: Fullname(dot),
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
							ServiceAccountName: ServiceAccountName(dot),
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
func PostInstallUpgradeEnvironmentVariables(dot *helmette.Dot) []corev1.EnvVar {
	envars := []corev1.EnvVar{}
	values := helmette.Unwrap[Values](dot.Values)

	if license := values.Enterprise.License; license != "" {
		envars = append(envars, corev1.EnvVar{
			Name:  "REDPANDA_LICENSE",
			Value: license,
		})
	} else if secretReference := values.Enterprise.LicenseSecretRef; secretReference != nil {
		envars = append(envars, corev1.EnvVar{
			Name: "REDPANDA_LICENSE",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: secretReference,
			},
		})
	}

	// include any authentication envvars as well.
	return bootstrapEnvVars(dot, envars)
}
