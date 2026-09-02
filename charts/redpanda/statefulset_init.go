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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

// Init container names.
const (
	// RedpandaConfiguratorContainerName is the user facing name of the
	// redpanda-configurator init container in the redpanda StatefulSet.
	RedpandaConfiguratorContainerName = "redpanda-configurator"
	// RedpandaTuningContainerName is the user facing name of the
	// tuning init container in the redpanda StatefulSet.
	RedpandaTuningContainerName = "tuning"
	// SetDataDirectoryOwnershipContainerName is the user facing name of the
	// set-datadir-ownership init container in the redpanda StatefulSet.
	SetDataDirectoryOwnershipContainerName = "set-datadir-ownership"
	// SetTieredStorageCacheOwnershipContainerName is the user facing name of the
	// set-tiered-storage-cache-dir-ownership init container in the redpanda StatefulSet.
	SetTieredStorageCacheOwnershipContainerName = "set-tiered-storage-cache-dir-ownership"
	// FSValidatorContainerName is the user facing name of the
	// fs-validator init container in the redpanda StatefulSet.
	FSValidatorContainerName = "fs-validator"
)

// Volume names shared by the chart's StatefulSet and the operator's
// multicluster (StretchCluster) renderer.
const (
	// ConfigVolumeName holds the per-pod redpanda.yaml produced by the
	// configurator init container.
	ConfigVolumeName = "config"
	// BaseConfigVolumeName holds the rendered-but-not-yet-per-pod config
	// mounted from the ConfigMap.
	BaseConfigVolumeName = "base-config"
	// DatadirVolumeName is the volume (and PVC) holding Redpanda's data
	// directory.
	DatadirVolumeName = "datadir"
	// ConfiguratorScriptsVolumeName holds the configurator.sh Secret.
	ConfiguratorScriptsVolumeName = "configurator"
	// FSValidatorScriptsVolumeName holds the fsValidator.sh Secret.
	FSValidatorScriptsVolumeName = "fs-validator"
)

// Mount paths shared by the chart's StatefulSet and the operator's
// multicluster (StretchCluster) renderer.
const (
	// RedpandaConfigMountPath is where Redpanda reads its final config from.
	RedpandaConfigMountPath = "/etc/redpanda"
	// BaseConfigMountPath is where the ConfigMap's base config is mounted for
	// the configurator and bootstrap init containers to read.
	BaseConfigMountPath = "/tmp/base-config"
	// DatadirMountPath is where Redpanda's data directory is mounted.
	DatadirMountPath = "/var/lib/redpanda/data"
)

// StatefulSetInitContainerRenderer renders the init containers that the Helm
// chart's StatefulSet and the operator's multicluster (StretchCluster)
// renderer have in common.
//
// It is deliberately inert: every field is a resolved scalar, a prebuilt
// slice, or an option struct whose presence decides whether a container is
// emitted. No method reads chart values or a broker pool, and nothing here
// defaults, gates, or inspects a domain string — resolving anything that
// requires interpreting a mount type, an enablement flag, or a pod template
// is the caller's job. That asymmetry is the point: it lets the two callers
// keep their very different notions of "a pool" without either leaking in
// here.
type StatefulSetInitContainerRenderer struct {
	// Image is the Redpanda image. It runs the tuning, fs-validator, and
	// configurator containers, all of which need rpk.
	Image string

	// InitImage is the minimal image used by the two chown containers. It
	// needs nothing but a shell.
	InitImage string

	// SidecarImage is the operator image, which provides the
	// `/redpanda-operator bootstrap` entrypoint.
	SidecarImage string

	// CommonMounts is prepended to the mounts of every container that reads
	// the cluster's config or certificates. It is not applied to the
	// host-tuner or bootstrap containers, which mount only what they need.
	CommonMounts []corev1.VolumeMount

	Tuning                      *TuningInitContainer
	DataDirOwnership            *DataDirOwnershipInitContainer
	FSValidator                 *FSValidatorInitContainer
	TieredStorageCacheOwnership *TieredStorageCacheOwnershipInitContainer
	Configurator                *ConfiguratorInitContainer
	Bootstrap                   *BootstrapInitContainer
}

// The init container option structs below double as enablement flags: a nil
// field means "don't emit this container", so the caller expresses its gating
// by constructing (or not constructing) an option rather than by handing the
// renderer a boolean it would have to interpret.

// TuningInitContainer configures the tuning init container, which runs
// `rpk redpanda tune all`. OnHost selects the chroot-into-/host variant that
// can reach the node's real block devices and sysctls; see [HostTunerScript()].
type TuningInitContainer struct {
	OnHost bool
}

// DataDirOwnershipInitContainer chowns the data directory to UID:GID, for
// storage backends that hand the volume over owned by root.
type DataDirOwnershipInitContainer struct {
	UID int64
	GID int64
}

// FSValidatorInitContainer asserts the data directory exists, is of
// ExpectedFS, and is writable before Redpanda starts. The script it runs is
// [FSValidatorSh].
type FSValidatorInitContainer struct {
	ExpectedFS string
}

// TieredStorageCacheOwnershipInitContainer creates the tiered storage cache
// directory and chowns it to UID:GID.
type TieredStorageCacheOwnershipInitContainer struct {
	UID int64
	GID int64

	// CacheDirectory is the path to create and chown.
	CacheDirectory string

	// CacheVolumeName is the volume the cache directory lives on. An empty
	// string means it lives on the data directory volume and so needs no
	// mount of its own; deciding that from a storage mount type is the
	// caller's job.
	CacheVolumeName string
}

// ConfiguratorInitContainer configures the container that turns the base
// config into this pod's redpanda.yaml, via [ConfiguratorPrologueSh] and
// friends.
type ConfiguratorInitContainer struct {
	// MountAPIToken projects the pod's ServiceAccount token in, which the
	// rack awareness block needs in order to read its Node.
	MountAPIToken bool

	// AdditionalEnv is appended to the four environment variables the script
	// itself requires, for callers that also need e.g. rpk's SASL
	// credentials in scope.
	AdditionalEnv []corev1.EnvVar
}

// BootstrapInitContainer configures the container that expands environment
// variables into bootstrap.yaml, so secrets referenced by the cluster config
// never have to be written into the ConfigMap.
type BootstrapInitContainer struct {
	// Env carries the values being substituted.
	Env []corev1.EnvVar

	// AdditionalCLIArgs is appended to the bootstrap subcommand's arguments.
	AdditionalCLIArgs []string
}

// Render returns every configured init container, in the order Kubernetes
// will run them. Containers whose option field is nil are omitted.
func (r StatefulSetInitContainerRenderer) Render() []corev1.Container {
	var containers []corev1.Container

	if r.Tuning != nil {
		if r.Tuning.OnHost {
			containers = append(containers, r.tuningOnHostContainer())
		} else {
			containers = append(containers, r.tuningContainer())
		}
	}

	if r.DataDirOwnership != nil {
		containers = append(containers, r.setDataDirOwnershipContainer(r.DataDirOwnership))
	}

	if r.FSValidator != nil {
		containers = append(containers, r.fsValidatorContainer(r.FSValidator))
	}

	if r.TieredStorageCacheOwnership != nil {
		containers = append(containers, r.setTieredStorageCacheDirOwnershipContainer(r.TieredStorageCacheOwnership))
	}

	if r.Configurator != nil {
		containers = append(containers, r.configuratorContainer(r.Configurator))
	}

	if r.Bootstrap != nil {
		containers = append(containers, r.bootstrapYamlTemplaterContainer(r.Bootstrap))
	}

	return containers
}

// mounts returns CommonMounts as a fresh slice. Taking a copy matters: the
// methods below each append their own mounts, and appending straight onto a
// shared slice would let one container's mounts land in another's backing
// array.
func (r StatefulSetInitContainerRenderer) mounts() []corev1.VolumeMount {
	var mounts []corev1.VolumeMount
	mounts = append(mounts, r.CommonMounts...)
	return mounts
}

// setDataDirOwnershipContainer returns the init container that chowns the data
// directory to UID:GID, for storage backends that hand the volume over as
// root.
func (r StatefulSetInitContainerRenderer) setDataDirOwnershipContainer(opts *DataDirOwnershipInitContainer) corev1.Container {
	return corev1.Container{
		Name:  SetDataDirectoryOwnershipContainerName,
		Image: r.InitImage,
		Command: []string{
			`/bin/sh`,
			`-c`,
			fmt.Sprintf(`chown %d:%d -R %s`, opts.UID, opts.GID, DatadirMountPath),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: append(
			r.mounts(),
			corev1.VolumeMount{
				Name:      DatadirVolumeName,
				MountPath: DatadirMountPath,
			},
		),
	}
}

// fsValidatorContainer returns the init container that asserts the data
// directory is present, of the expected filesystem type, and writable before
// Redpanda starts. The script it runs is [FSValidatorSh].
func (r StatefulSetInitContainerRenderer) fsValidatorContainer(opts *FSValidatorInitContainer) corev1.Container {
	return corev1.Container{
		Name:    FSValidatorContainerName,
		Image:   r.Image,
		Command: []string{`/bin/sh`},
		Args: []string{
			`-c`,
			fmt.Sprintf(`trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!`, opts.ExpectedFS),
		},
		VolumeMounts: append(
			r.mounts(),
			corev1.VolumeMount{
				Name:      FSValidatorScriptsVolumeName,
				MountPath: `/etc/secrets/fs-validator/scripts/`,
			},
			corev1.VolumeMount{
				Name:      DatadirVolumeName,
				MountPath: DatadirMountPath,
			},
		),
	}
}

// configuratorContainer returns the init container that runs
// [ConfiguratorPrologueSh] and friends to turn the base config into this
// pod's redpanda.yaml.
//
// mountAPIToken projects the pod's ServiceAccount token in, which the rack
// awareness block needs to read its Node. additionalEnv is appended to the
// four environment variables the script itself requires, for callers that
// also need e.g. rpk's SASL credentials in scope.
func (r StatefulSetInitContainerRenderer) configuratorContainer(opts *ConfiguratorInitContainer) corev1.Container {
	volMounts := append(
		r.mounts(),
		corev1.VolumeMount{
			Name:      ConfigVolumeName,
			MountPath: RedpandaConfigMountPath,
		},
		corev1.VolumeMount{
			Name:      BaseConfigVolumeName,
			MountPath: BaseConfigMountPath,
		},
		corev1.VolumeMount{
			Name:      ConfiguratorScriptsVolumeName,
			MountPath: "/etc/secrets/configurator/scripts/",
		},
	)

	if opts.MountAPIToken {
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      ServiceAccountVolumeName,
			MountPath: DefaultAPITokenMountPath,
			ReadOnly:  true,
		})
	}

	env := []corev1.EnvVar{
		{
			Name:  "CONFIGURATOR_SCRIPT",
			Value: "/etc/secrets/configurator/scripts/configurator.sh",
		},
		{
			Name: "SERVICE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
				ResourceFieldRef: nil,
				ConfigMapKeyRef:  nil,
				SecretKeyRef:     nil,
			},
		},
		{
			Name: "KUBERNETES_NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
			},
		},
		{
			Name: "HOST_IP_ADDRESS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "status.hostIP",
				},
			},
		},
	}

	return corev1.Container{
		Name:  RedpandaConfiguratorContainerName,
		Image: r.Image,
		Command: []string{
			`/bin/bash`,
			`-c`,
			`trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`,
		},
		Env:          append(env, opts.AdditionalEnv...),
		VolumeMounts: volMounts,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
	}
}

// setTieredStorageCacheDirOwnershipContainer returns the init container that
// creates the tiered-storage cache directory and chowns it to UID:GID.
//
// cacheVolumeName is the volume the cache directory lives on. An empty string
// means it lives on the data directory volume and needs no mount of its own;
// resolving that from a mount type is the caller's job.
func (r StatefulSetInitContainerRenderer) setTieredStorageCacheDirOwnershipContainer(opts *TieredStorageCacheOwnershipInitContainer) corev1.Container {
	volMounts := append(
		r.mounts(),
		corev1.VolumeMount{
			Name:      DatadirVolumeName,
			MountPath: DatadirMountPath,
		},
	)

	if opts.CacheVolumeName != "" {
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      opts.CacheVolumeName,
			MountPath: opts.CacheDirectory,
		})
	}

	return corev1.Container{
		Name:  SetTieredStorageCacheOwnershipContainerName,
		Image: r.InitImage,
		Command: []string{
			`/bin/sh`,
			`-c`,
			fmt.Sprintf(`mkdir -p %s; chown %d:%d -R %s`, opts.CacheDirectory, opts.UID, opts.GID, opts.CacheDirectory),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: volMounts,
	}
}

// bootstrapYamlTemplaterContainer returns the init container that expands
// environment variables into bootstrap.yaml, so that secrets referenced by
// the cluster config never have to be written into the ConfigMap.
//
// env carries the values being substituted; additionalCLIArgs is appended to
// the bootstrap subcommand's arguments.
func (r StatefulSetInitContainerRenderer) bootstrapYamlTemplaterContainer(opts *BootstrapInitContainer) corev1.Container {
	return corev1.Container{
		Name:  "bootstrap-yaml-envsubst",
		Image: r.SidecarImage,
		Command: append([]string{
			"/redpanda-operator",
			"bootstrap",
			"--in-dir",
			BaseConfigMountPath,
			"--out-dir",
			"/tmp/config",
		}, opts.AdditionalCLIArgs...),
		Env: opts.Env,
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
			{Name: ConfigVolumeName, MountPath: "/tmp/config/"},
			{Name: BaseConfigVolumeName, MountPath: BaseConfigMountPath + "/"},
		},
	}
}

// tuningContainer returns the in-pod tuning init container, which runs
// `rpk redpanda tune all` against the pod's own namespaces. Callers gate this
// on their tune_aio_events setting, and use TuningOnHostContainer instead when
// host tuners are requested.
func (r StatefulSetInitContainerRenderer) tuningContainer() corev1.Container {
	return corev1.Container{
		Name:    RedpandaTuningContainerName,
		Image:   r.Image,
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
			r.mounts(),
			corev1.VolumeMount{
				Name:      BaseConfigVolumeName,
				MountPath: RedpandaConfigMountPath,
			},
			corev1.VolumeMount{
				Name:      DatadirVolumeName,
				MountPath: DatadirMountPath,
			},
		),
	}
}

// tuningOnHostContainer returns the tuning init container
// that runs `rpk redpanda tune all` in a chroot to the host filesystem.
//
// Why a chroot: the default tuning container runs rpk inside the pod's
// own filesystem and namespaces, so the disk_irq / disk_scheduler /
// disk_nomerges / net tuners can't find host block devices in /sys/block
// or write host sysctls in /proc/sys/net. By chrooting into /host (which
// has the host's /sys, /proc, /usr, ... bind-mounted) and using
// `nsenter -t 1 -n` to enter the host network namespace, rpk sees the
// real host and the tuners apply for real.
//
// Workarounds layered in by this function (see HostTunerScript for the
// script-side ones):
//   - cp (under umask 077) + sed the rendered redpanda.yaml into
//     /var/tmp and inject `redpanda.data_directory` so the disk tuners
//     have a path to resolve. The base chart deliberately omits
//     data_directory (the broker doesn't need it) but rpk's tuner
//     refuses to combine `--dirs` with `--config`, so the value must
//     live in the file.
//   - busctl call into the host's systemd to try-restart irqbalance
//     after rpk rewrites IRQ affinity (systemctl can't traverse a
//     chroot). No non-systemd fallback — see HostTunerScript for why
//     none can work without hostPID.
//   - a `which` shim written into /opt/redpanda/bin (bind-mounted into
//     the chroot, first on PATH): rpk's fstrim tuner shells out to
//     `which`, and some minimal node images (AKS Ubuntu) ship a broken
//     or missing /usr/bin/which.
//
// Pre-conditions for this to work:
//   - one Redpanda pod per node (anti-affinity); concurrent tuners race
//     on the same kernel parameters.
//   - the pod's ServiceAccount is bound to an SCC / PSA level that
//     allows hostPath volumes and privileged: true.
//
// See also [HostTunerScript()] and [HostTunerVolumeMounts] for why it looks the
// way it does and what it requires of the node.
func (r StatefulSetInitContainerRenderer) tuningOnHostContainer() corev1.Container {
	return corev1.Container{
		Name:    RedpandaTuningContainerName,
		Image:   r.Image,
		Command: []string{`/bin/bash`, `-c`, HostTunerScript()},
		SecurityContext: &corev1.SecurityContext{
			// privileged: true already grants every capability;
			// explicit Add entries would be redundant noise.
			Privileged:   ptr.To(true),
			RunAsNonRoot: ptr.To(false),
			RunAsUser:    ptr.To(int64(0)),
			RunAsGroup:   ptr.To(int64(0)),
		},
		VolumeMounts: HostTunerVolumeMounts(),
	}
}
