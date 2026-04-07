// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// statefulSetInitContainers returns the init containers for the StatefulSet.
func statefulSetInitContainers(state *RenderState, pool *redpandav1alpha2.NodePool) []corev1.Container {
	var containers []corev1.Container

	if state.Spec().Tuning.IsTuneAioEventsEnabled() {
		containers = append(containers, statefulSetInitContainerTuning(state, pool))
	}

	if pool.Spec.InitContainers != nil && pool.Spec.InitContainers.SetDataDirOwnership.IsEnabled() {
		containers = append(containers, statefulSetInitContainerSetDataDirOwnership(state, pool))
	}

	if pool.Spec.InitContainers != nil && pool.Spec.InitContainers.FSValidator.IsEnabled() {
		containers = append(containers, statefulSetInitContainerFSValidator(state, pool))
	}

	if state.Spec().TieredMountType() != "none" {
		containers = append(containers, statefulSetInitContainerSetTieredStorageCacheDirOwnership(state, pool))
	}

	containers = append(containers, statefulSetInitContainerConfigurator(state, pool))

	// Compute bootstrap env vars needed by the envsubst init container.
	bootstrap := bootstrapContents(state)
	containers = append(containers, bootstrapYamlTemplater(pool, bootstrap.envVars))

	return containers
}

func statefulSetInitContainerTuning(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	return corev1.Container{
		Name:    redpandaTuningContainerName,
		Image:   pool.RedpandaImage(),
		Command: []string{`/bin/bash`, `-c`, `rpk redpanda tune all`},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{`SYS_RESOURCE`},
			},
			Privileged:   ptr.To(true),
			RunAsNonRoot: ptr.To(false),
			RunAsUser:    ptr.To(int64(0)),
			RunAsGroup:   ptr.To(int64(0)),
		},
		VolumeMounts: append(
			state.commonMounts(),
			corev1.VolumeMount{Name: baseConfigVolumeName, MountPath: redpandaConfigMountPath},
			corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
		),
	}
}

func statefulSetInitContainerSetDataDirOwnership(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	return corev1.Container{
		Name:    setDataDirectoryOwnershipContainerName,
		Image:   pool.InitImage(),
		Command: []string{`/bin/sh`, `-c`, fmt.Sprintf(`chown %d:%d -R %s`, redpandaUserID, redpandaGroupID, datadirMountPath)},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: append(
			state.commonMounts(),
			corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
		),
	}
}

func statefulSetInitContainerFSValidator(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	var fsValidator *redpandav1alpha2.PoolFSValidator
	if pool.Spec.InitContainers != nil {
		fsValidator = pool.Spec.InitContainers.FSValidator
	}
	expectedFS := fsValidator.GetExpectedFS()

	return corev1.Container{
		Name:    fsValidatorContainerName,
		Image:   pool.RedpandaImage(),
		Command: []string{`/bin/sh`},
		Args: []string{
			`-c`,
			fmt.Sprintf(`trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!`, expectedFS),
		},
		VolumeMounts: append(
			state.commonMounts(),
			corev1.VolumeMount{Name: fmt.Sprintf(`%.49s-fs-validator`, state.fullname()), MountPath: `/etc/secrets/fs-validator/scripts/`},
			corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
		),
	}
}

func statefulSetInitContainerConfigurator(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	volMounts := state.commonMounts()
	volMounts = append(volMounts,
		corev1.VolumeMount{Name: configVolumeName, MountPath: redpandaConfigMountPath},
		corev1.VolumeMount{Name: baseConfigVolumeName, MountPath: baseConfigMountPath},
		corev1.VolumeMount{Name: fmt.Sprintf(`%.51s-configurator`, state.fullname()), MountPath: "/etc/secrets/configurator/scripts/"},
	)

	if state.Spec().RackAwareness.IsEnabled() {
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      serviceAccountVolumeName,
			MountPath: defaultAPITokenMountPath,
			ReadOnly:  true,
		})
	}

	return corev1.Container{
		Name:  redpandaConfiguratorContainerName,
		Image: pool.RedpandaImage(),
		Command: []string{
			`/bin/bash`, `-c`,
			`trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`,
		},
		Env: []corev1.EnvVar{
			{Name: "CONFIGURATOR_SCRIPT", Value: "/etc/secrets/configurator/scripts/configurator.sh"},
			{
				Name: "SERVICE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "KUBERNETES_NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
				},
			},
			{
				Name: "HOST_IP_ADDRESS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "status.hostIP"},
				},
			},
		},
		VolumeMounts: volMounts,
	}
}

// bootstrapYamlTemplater returns an init container that templates environment variables
// into bootstrap.yaml.
func bootstrapYamlTemplater(pool *redpandav1alpha2.NodePool, envVars []corev1.EnvVar) corev1.Container {
	image := pool.SidecarImage()

	var cliArgs []string
	if pool.Spec.InitContainers != nil && pool.Spec.InitContainers.Configurator != nil {
		cliArgs = pool.Spec.InitContainers.Configurator.AdditionalCLIArgs
	}

	return corev1.Container{
		Name:  "bootstrap-yaml-envsubst",
		Image: image,
		Command: append([]string{
			"/redpanda-operator",
			"bootstrap",
			"--in-dir", baseConfigMountPath,
			"--out-dir", "/tmp/config",
		}, cliArgs...),
		Env: envVars,
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
		VolumeMounts: []corev1.VolumeMount{
			{Name: configVolumeName, MountPath: "/tmp/config/"},
			{Name: baseConfigVolumeName, MountPath: baseConfigMountPath + "/"},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			RunAsNonRoot:             ptr.To(true),
		},
	}
}

// statefulSetInitContainerSetTieredStorageCacheDirOwnership returns an init container
// that creates and chowns the tiered storage cache directory.
func statefulSetInitContainerSetTieredStorageCacheDirOwnership(state *RenderState, pool *redpandav1alpha2.NodePool) corev1.Container {
	cacheDir := state.Spec().TieredCacheDirectory()

	volMounts := state.commonMounts()
	volMounts = append(volMounts,
		corev1.VolumeMount{Name: datadirVolumeName, MountPath: datadirMountPath},
	)
	mountType := state.Spec().TieredMountType()
	if mountType != "none" {
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      state.Spec().TieredStorageVolumeName(),
			MountPath: cacheDir,
		})
	}

	return corev1.Container{
		Name:  "set-tiered-storage-cache-dir-ownership",
		Image: pool.InitImage(),
		Command: []string{
			"/bin/sh", "-c",
			fmt.Sprintf("mkdir -p %s; chown %d:%d -R %s", cacheDir, redpandaUserID, redpandaGroupID, cacheDir),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: volMounts,
	}
}
