// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_statefulset_init.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
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
// can reach the node's real block devices and sysctls; see [hostTunerScript].
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
// See also [hostTunerScript] and [hostTunerVolumeMounts] for why it looks the
// way it does and what it requires of the node.
func (r StatefulSetInitContainerRenderer) tuningOnHostContainer() corev1.Container {
	return corev1.Container{
		Name:    RedpandaTuningContainerName,
		Image:   r.Image,
		Command: []string{`/bin/bash`, `-c`, hostTunerScript},
		SecurityContext: &corev1.SecurityContext{
			// privileged: true already grants every capability;
			// explicit Add entries would be redundant noise.
			Privileged:   ptr.To(true),
			RunAsNonRoot: ptr.To(false),
			RunAsUser:    ptr.To(int64(0)),
			RunAsGroup:   ptr.To(int64(0)),
		},
		VolumeMounts: hostTunerVolumeMounts(),
	}
}

// hostTunerDirs returns the host filesystem directories bind-mounted
// into the tuning container so that `rpk redpanda tune all` can chroot
// in and see the host's /sys, /proc, NIC devices, block devices, and
// rpk binary. Kept as a list (not whole-/) on purpose: bind-mounting /
// into /host creates mount-loops with /opt/redpanda. See
// https://redpandadata.atlassian.net/browse/CORE-13685
// bin and sbin matter for non-usr-merged hosts (GKE's COS): there
// /bin is a real directory — bash lives at /bin/bash, NOT
// /usr/bin/bash — so without these mounts the chroot has no shell at
// all and tuning silently no-ops. On usr-merged hosts (Ubuntu,
// AL2023) /bin and /sbin are symlinks into /usr, and the bind mount
// just resolves to the same content.
func hostTunerDirs() []string {
	return []string{"bin", "sbin", "sys", "proc", "etc", "usr", "lib", "lib64", "dev", "var", "run"}
}

// hostTunerStateFilePath is where rpk persists the net tuner's cpuset
// state on the host (rpk's own default path; see
// DefaultNodeTunerStateFile in rpk). The tuning init container writes
// it through the /host/var and /host/run bind mounts, and the broker
// container mounts it read-only at the same path so `rpk redpanda
// start` picks the cpuset up without any extra flag. /var/run is tmpfs,
// so state never outlives a node reboot.
const hostTunerStateFilePath = "/var/run/redpanda_node_tuner_state.yaml"

// hostTunerVolumeMounts returns the volume mounts for the host-mode
// tuning init container. Exported so the multicluster (StretchCluster)
// renderer can reuse it; the "base-config" and "datadir" volume names
// are identical in both renderers.
//
// Mount decisions:
//   - HostToContainer, NOT Bidirectional: the chroot'd rpk runs in this
//     container's mount namespace, so it already sees every mount made
//     here (the /opt/redpanda bind, the datadir PVC) without any
//     propagation. Bidirectional would additionally propagate the
//     datadir PVC mount (which lives under the host-var subtree at
//     /host/var/lib/redpanda/data) back onto the host's real
//     /var/lib/redpanda/data — and that host-side mount outlives the
//     pod, stacking one leaked mount per pod incarnation.
//   - /bin, /sbin, /usr, /lib and /lib64 are mounted read-only: they
//     exist purely to give the chroot a shell, coreutils and shared
//     libraries. Everything the tuners write lives under /sys, /proc,
//     /etc (fstrim systemd units), /dev, /var and /run, which stay
//     writable.
//   - The tuner state file needs no dedicated mount here: rpk writes
//     its default state path (HostTunerStateFilePath, under /var/run)
//     straight through the /host/var and /host/run binds.
func hostTunerVolumeMounts() []corev1.VolumeMount {
	readOnlyDirs := map[string]bool{
		"bin":   true,
		"sbin":  true,
		"usr":   true,
		"lib":   true,
		"lib64": true,
	}
	mounts := []corev1.VolumeMount{}
	for _, dir := range hostTunerDirs() {
		mounts = append(mounts, corev1.VolumeMount{
			Name:             fmt.Sprintf("host-%s", dir),
			MountPath:        fmt.Sprintf("/host/%s", dir),
			ReadOnly:         readOnlyDirs[dir],
			MountPropagation: ptr.To(corev1.MountPropagationHostToContainer),
		})
	}
	mounts = append(mounts,
		corev1.VolumeMount{
			Name:      "base-config",
			MountPath: "/host/redpanda_etc",
		},
		corev1.VolumeMount{
			Name:      "datadir",
			MountPath: "/host/var/lib/redpanda/data",
		},
	)
	return mounts
}

// HostTunerStateVolumeMount is the broker container's read-only view of
// the tuner state file, mounted at rpk's default state path so `rpk
// redpanda start` reads the net tuner's cpuset (written by the tuning
// init container, which runs first) without any extra flag. In
// dedicated-IRQ modes (sq/sq_split) this keeps reactor shards off the
// CPUs pinned to NIC IRQs; in mq mode (typical cloud VMs) rpk writes an
// empty file, which its state reader documents as "no cpuset". Because
// no flag is involved, broker images whose rpk predates tuner-state
// support simply ignore the file. Exported for the multicluster
// (StretchCluster) renderer.
func HostTunerStateVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      "host-tuner-state",
		MountPath: hostTunerStateFilePath,
		ReadOnly:  true,
	}
}

// hostTunerScript returns the bash script run by the host-mode tuning
// init container. It builds a chroot to the host filesystem and invokes
// `rpk redpanda tune all` inside the host's network namespace so the
// tuners that need /sys, /proc, host NICs and host block devices can
// actually apply. Exported so the multicluster (StretchCluster)
// renderer can reuse it.
//
// Failure policy (deliberately two-tier):
//   - Everything up to and including `rpk redpanda tune list` is
//     fail-fast (set -euo pipefail): a broken bind mount, an unusable
//     chroot, a config copy that failed, or a config rpk cannot parse
//     means ZERO tuners would run, and the init container must
//     crashloop loudly instead of letting the pod go Ready with the
//     opt-in feature silently doing nothing.
//   - The `rpk redpanda tune all` exit code alone is tolerated (with a
//     loud warning): rpk exits non-zero when any single enabled tuner
//     fails, and individual tuners legitimately fail on specific
//     hosts (e.g. disk_irq on AL2023 arm64 metal, which lacks a
//     writable smp_affinity for IRQ 0) while every other tuner applied
//     fine. Crashlooping the broker over one degraded tuner would be
//     worse than running with it untuned.
//
// Workarounds layered in:
//   - The chart-rendered redpanda.yaml omits `redpanda.data_directory`
//     (the broker doesn't need it). rpk's disk tuners do need it, and
//     rpk refuses to combine `--dirs` with `--config`. So we cp a
//     working copy into /var/tmp (because /tmp is not bind-mounted from
//     the host) and inject the key — guarded by a grep so a config that
//     already sets data_directory (e.g. via config.node) doesn't end up
//     with a duplicate YAML key that rpk would reject.
//   - The copy is made under umask 077 and removed on exit: the
//     rendered config can carry rpk SASL credentials, and /var/tmp on
//     the host outlives both the pod and chart uninstall.
//   - busctl TryRestartUnit into the host's systemd so a running
//     irqbalance doesn't undo rpk's IRQ affinity work (systemctl can't
//     traverse a chroot; TryRestartUnit, unlike RestartUnit, won't
//     start an intentionally-stopped unit). This is deliberately the
//     ONLY mechanism: there is no process-kill fallback for
//     non-systemd hosts because none can work from here — kill(2)
//     resolves PIDs in the caller's PID namespace (host PIDs read from
//     the bind-mounted /proc always ESRCH), and setns(2) into the
//     host's PID namespace is an ancestor of the container's, which
//     the kernel rejects with EINVAL (setns(2): the target PID
//     namespace must be a descendant of the caller's). Reaching host
//     irqbalance on a non-systemd node would require hostPID on the
//     whole pod, which is not worth it: every mainstream node image
//     (COS, AL2023, Ubuntu, Bottlerocket) runs systemd, and on hosts
//     without an irqbalance unit TryRestartUnit is a clean no-op.
//   - a `which` shim in /opt/redpanda/bin for rpk's fstrim tuner, which
//     shells out to `which` (missing/broken on minimal node images).
//   - No --node-tuner-state-path: rpk's default state path
//     (HostTunerStateFilePath, under /var/run) already resolves to the
//     host's tmpfs through the /host/var and /host/run binds, and
//     passing only long-stable flags means an older rpk in a pinned
//     image can never fail with `unknown flag` (which would previously
//     have been swallowed and left zero tuners applied).
//
// The chroot shell is /bin/bash (not /usr/bin/bash): COS is not
// usr-merged and only has /bin/bash; usr-merged hosts resolve
// /bin/bash to the same binary via the /bin symlink.
const hostTunerScript = `set -xeuo pipefail
umask 077
mkdir -p /host/opt/redpanda
mount --bind /opt/redpanda /host/opt/redpanda
printf '#!/bin/sh\ncommand -v "$@"\n' > /opt/redpanda/bin/which
chmod +x /opt/redpanda/bin/which
chroot /host /bin/bash -c 'true' || { echo "FATAL: cannot exec /bin/bash inside the /host chroot; this node's filesystem layout is not supported by tuning.apply_host_tuners" >&2; exit 1; }
trap 'rm -f /host/var/tmp/redpanda-tune.yaml' EXIT
cp /host/redpanda_etc/redpanda.yaml /host/var/tmp/redpanda-tune.yaml
grep -q 'data_directory:' /host/var/tmp/redpanda-tune.yaml || sed -i 's|^redpanda:|redpanda:\n  data_directory: /var/lib/redpanda/data|' /host/var/tmp/redpanda-tune.yaml
chroot /host /bin/bash -c '
  set -xeuo pipefail
  export PATH="/opt/redpanda/bin:$PATH"
  nsenter -t 1 -n /opt/redpanda/bin/rpk redpanda tune list --config /var/tmp/redpanda-tune.yaml
  rc=0
  nsenter -t 1 -n /opt/redpanda/bin/rpk redpanda tune all --config /var/tmp/redpanda-tune.yaml -v || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "WARNING: rpk redpanda tune all exited $rc; at least one enabled tuner failed to apply (see output above). Not blocking broker startup over a single degraded tuner." >&2
  fi
  busctl call org.freedesktop.systemd1 /org/freedesktop/systemd1 \
    org.freedesktop.systemd1.Manager TryRestartUnit ss "irqbalance.service" "replace" \
    || true
'
`
