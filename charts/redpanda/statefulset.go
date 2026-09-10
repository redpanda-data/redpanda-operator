// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_statefulset.go.tpl
package redpanda

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	// TrustStoreMountPath is the absolute path at which the
	// [corev1.VolumeProjection] of truststores will be mounted to the redpanda
	// container. (Without a trailing slash)
	TrustStoreMountPath = "/etc/truststores"

	// Injected bound service account token expiration which triggers monitoring of its time-bound feature.
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/ae53151cb4e6fbba8bb78a2ef0b48a7c32a0a067/pkg/serviceaccount/claims.go#L38-L39
	tokenExpirationSeconds = 60*60 + 7

	// ServiceAccountVolumeName is the prefix name that will be added to volumes that mount ServiceAccount secrets
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L52-L53
	ServiceAccountVolumeName = "kube-api-access"

	// DefaultAPITokenMountPath is the path that ServiceAccountToken secrets are automounted to.
	// The token file would then be accessible at /var/run/secrets/kubernetes.io/serviceaccount
	// Reference
	// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L55-L57
	//nolint:gosec
	DefaultAPITokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	NodePoolLabelName       = "cluster.redpanda.com/nodepool-name"
	NodePoolLabelGeneration = "cluster.redpanda.com/nodepool-generation"
)

// statefulSetRedpandaEnv returns the environment variables for the Redpanda
// container of the Redpanda Statefulset.
func statefulSetRedpandaEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "SERVICE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		},
	}
}

// ClusterPodLabelsSelector returns the labels that apply to every broker
// pod in a cluster, regardless of node pool.
func ClusterPodLabelsSelector(state *RenderState) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance": state.Release.Name,
		"app.kubernetes.io/name":     Name(state),
	}
}

// StatefulSetPodLabelsSelector returns the label selector for the Redpanda StatefulSet.
// If this helm release is an upgrade, the existing statefulset's label selector will be used as it's an immutable field.
func StatefulSetPodLabelsSelector(state *RenderState, pool Pool) map[string]string {
	// StatefulSets cannot change their selector. Use the existing one even if it's broken.
	// New installs will get better selectors.
	// NB: the name check means we only memoize the default StatefulSet selector
	if state.StatefulSetSelector != nil && pool.Name == "" {
		return state.StatefulSetSelector
	}

	additionalSelectorLabels := map[string]string{}
	if pool.Statefulset.AdditionalSelectorLabels != nil {
		additionalSelectorLabels = pool.Statefulset.AdditionalSelectorLabels
	}

	name := fmt.Sprintf("%s%s", Name(state), pool.Suffix())
	component := fmt.Sprintf("%s-statefulset", strings.TrimSuffix(helmette.Trunc(51, name), "-"))

	defaults := map[string]string{
		"app.kubernetes.io/component": component,
	}

	return helmette.Merge(additionalSelectorLabels, defaults, ClusterPodLabelsSelector(state))
}

// StatefulSetPodLabels returns the label that includes label selector for the Redpanda PodTemplate.
// If this helm release is an upgrade, the existing statefulset's pod template labels will be used as it's an immutable field.
func StatefulSetPodLabels(state *RenderState, pool Pool) map[string]string {
	// NB: the name check means we only memoize the default StatefulSet labels
	if state.StatefulSetPodLabels != nil && pool.Name == "" {
		return state.StatefulSetPodLabels
	}

	statefulSetLabels := map[string]string{}
	if pool.Statefulset.PodTemplate.Labels != nil {
		statefulSetLabels = pool.Statefulset.PodTemplate.Labels
	}

	defaults := map[string]string{
		"redpanda.com/poddisruptionbudget": Fullname(state),
		// the following label should really be part of our selector for services, etc. but adding it
		// currently breaks backwards compatibility with our services during upgrades
		"cluster.redpanda.com/broker": "true",
	}

	return helmette.Merge(statefulSetLabels, StatefulSetPodLabelsSelector(state, pool), defaults, FullLabels(state))
}

// StatefulSetVolumes returns the [corev1.Volume]s for the Redpanda StatefulSet.
func StatefulSetVolumes(state *RenderState, pool Pool) []corev1.Volume {
	fullname := Fullname(state)
	poolFullname := fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())
	volumes := CommonVolumes(state)

	// NOTE and tiered-storage-dir are NOT in this
	// function. TODO: Migrate them into this function.
	volumes = append(volumes, []corev1.Volume{
		{
			Name: "lifecycle-scripts",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.50s-sts-lifecycle", fullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
		{
			Name: "base-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: poolFullname},
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: fmt.Sprintf("%.51s-configurator", fullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.51s-configurator", poolFullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		},
	}...)

	if pool.Statefulset.InitContainers.FSValidator.Enabled {
		volumes = append(volumes, corev1.Volume{
			Name: fmt.Sprintf("%.49s-fs-validator", fullname),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  fmt.Sprintf("%.49s-fs-validator", poolFullname),
					DefaultMode: ptr.To[int32](0o775),
				},
			},
		})
	}

	if vol := state.Values.Listeners.TrustStoreVolume(&state.Values.TLS); vol != nil {
		volumes = append(volumes, *vol)
	}

	volumes = append(volumes, statefulSetVolumeDataDir(state))

	if v := statefulSetVolumeTieredStorageDir(state); v != nil {
		volumes = append(volumes, *v)
	}

	volumes = append(volumes, kubeTokenAPIVolume(ServiceAccountVolumeName))

	if state.Values.Tuning.TuneAIOEvents && state.Values.Tuning.ApplyHostTuners {
		volumes = append(volumes, HostTunerVolumes()...)
	}

	return volumes
}

// HostTunerVolumes returns the hostPath volumes used by the host-mode
// tuning init container (and, for the tuner state file, the broker
// container). Bound only when Tuning.ApplyHostTuners is true. Exported
// so the multicluster (StretchCluster) renderer can reuse it.
//
// Volume types are deliberately strict:
//   - Every directory except /lib64 uses HostPathDirectory: a node
//     missing one of these has a filesystem layout this feature cannot
//     work on, and the pod must fail admission with an explicit
//     FailedMount event instead of kubelet silently mkdir'ing paths
//     like /etc or /usr on the host root filesystem (OrCreate would
//     mutate the host at pod-admission time, and on read-only-root
//     distros wedge the pod in ContainerCreating with a less obvious
//     error).
//   - /lib64 keeps HostPathDirectoryOrCreate: it is the ELF interpreter
//     directory on amd64 (required, and a real directory on
//     non-usr-merged hosts such as COS, so it cannot be synthesized
//     from /usr), but it legitimately does not exist on arm64 hosts.
//     A StatefulSet has one pod spec for all nodes, so per-arch
//     conditional volumes are impossible; OrCreate is the only
//     mechanism that tolerates both. The cost is bounded and known: on
//     arm64 nodes kubelet creates an empty /lib64 directory, which no
//     arm64 binary ever consults.
//   - The tuner state file uses HostPathFileOrCreate. rpk explicitly
//     supports this: its state reader treats the empty file kubelet
//     creates as "no state" (see readTunerConfigCpuset in rpk). The
//     file lives in /var/run (tmpfs) so it never survives a reboot.
//
// Operators using OpenShift SCCs need to allow `hostPath` in the SCC's
// volumes list and add these paths to `allowedHostPaths` (or use the
// built-in `privileged` SCC). On PSA clusters, the namespace must be
// labeled `privileged`.
func HostTunerVolumes() []corev1.Volume {
	vols := []corev1.Volume{}
	for _, dir := range HostTunerDirs() {
		hostPathType := corev1.HostPathDirectory
		if dir == "lib64" {
			hostPathType = corev1.HostPathDirectoryOrCreate
		}
		vols = append(vols, corev1.Volume{
			Name: fmt.Sprintf("host-%s", dir),
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: fmt.Sprintf("/%s", dir),
					Type: ptr.To(hostPathType),
				},
			},
		})
	}
	vols = append(vols, corev1.Volume{
		Name: "host-tuner-state",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: HostTunerStateFilePath,
				Type: ptr.To(corev1.HostPathFileOrCreate),
			},
		},
	})
	return vols
}

// kubeTokenAPIVolume is a slightly changed variant of
// https://github.com/kubernetes/kubernetes/blob/c6669ea7d61af98da3a2aa8c1d2cdc9c2c57080a/plugin/pkg/admission/serviceaccount/admission.go#L484-L524
// Upstream creates Projected Volume Source, but this function returns Volume with provided name.
// Also const are renamed.
func kubeTokenAPIVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				// explicitly set default value, see https://github.com/kubernetes/kubernetes/issues/104464
				DefaultMode: ptr.To(corev1.ProjectedVolumeSourceDefaultMode),
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Path:              "token",
							ExpirationSeconds: ptr.To(int64(tokenExpirationSeconds)),
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "kube-root-ca.crt",
							},
							Items: []corev1.KeyToPath{
								{
									Key:  "ca.crt",
									Path: "ca.crt",
								},
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "namespace",
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func statefulSetVolumeDataDir(state *RenderState) corev1.Volume {
	datadirSource := corev1.VolumeSource{
		EmptyDir: &corev1.EmptyDirVolumeSource{},
	}
	if state.Values.Storage.PersistentVolume.Enabled {
		datadirSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "datadir",
			},
		}
	} else if state.Values.Storage.HostPath != "" {
		datadirSource = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: state.Values.Storage.HostPath,
			},
		}
	}
	return corev1.Volume{
		Name:         "datadir",
		VolumeSource: datadirSource,
	}
}

func statefulSetVolumeTieredStorageDir(state *RenderState) *corev1.Volume {
	if !state.Values.Storage.IsTieredStorageEnabled() {
		return nil
	}

	tieredType := state.Values.Storage.TieredMountType()
	if tieredType == "none" || tieredType == "persistentVolume" {
		return nil
	}

	if tieredType == "hostPath" {
		return &corev1.Volume{
			Name: "tiered-storage-dir",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: state.Values.Storage.GetTieredStorageHostPath(),
				},
			},
		}
	}

	return &corev1.Volume{
		Name: "tiered-storage-dir",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				SizeLimit: state.Values.Storage.GetTieredStorageConfig().CloudStorageCacheSize(),
			},
		},
	}
}

// StatefulSetRedpandaMounts returns the VolumeMounts for the Redpanda
// Container of the Redpanda StatefulSet.
func StatefulSetVolumeMounts(state *RenderState) []corev1.VolumeMount {
	mounts := CommonMounts(state)

	mounts = append(mounts, []corev1.VolumeMount{
		{Name: "config", MountPath: "/etc/redpanda"},
		{Name: "base-config", MountPath: "/tmp/base-config"},
		{Name: "lifecycle-scripts", MountPath: "/var/lifecycle"},
		{Name: "datadir", MountPath: "/var/lib/redpanda/data"},
		{Name: ServiceAccountVolumeName, MountPath: DefaultAPITokenMountPath, ReadOnly: true},
	}...)

	if len(state.Values.Listeners.TrustStores(&state.Values.TLS)) > 0 {
		mounts = append(
			mounts,
			corev1.VolumeMount{Name: "truststores", MountPath: TrustStoreMountPath, ReadOnly: true},
		)
	}

	if state.Values.Tuning.TuneAIOEvents && state.Values.Tuning.ApplyHostTuners {
		mounts = append(mounts, HostTunerStateVolumeMount())
	}

	return mounts
}

func StatefulSetInitContainers(state *RenderState, pool Pool) []corev1.Container {
	var containers []corev1.Container
	if c := statefulSetInitContainerTuning(state); c != nil {
		containers = append(containers, *c)
	}
	if c := statefulSetInitContainerSetDataDirOwnership(state, pool); c != nil {
		containers = append(containers, *c)
	}
	if c := statefulSetInitContainerFSValidator(state, pool); c != nil {
		containers = append(containers, *c)
	}
	if c := statefulSetInitContainerSetTieredStorageCacheDirOwnership(state, pool); c != nil {
		containers = append(containers, *c)
	}
	containers = append(containers, *statefulSetInitContainerConfigurator(state))
	containers = append(containers, bootstrapYamlTemplater(state, pool.Statefulset))
	return containers
}

// HostTunerDirs returns the host filesystem directories bind-mounted
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
//
// Exported so the multicluster (StretchCluster) renderer can reuse it.
func HostTunerDirs() []string {
	return []string{"bin", "sbin", "sys", "proc", "etc", "usr", "lib", "lib64", "dev", "var", "run"}
}

// HostTunerStateFilePath is where rpk persists the net tuner's cpuset
// state on the host (rpk's own default path; see
// DefaultNodeTunerStateFile in rpk). The tuning init container writes
// it through the /host/var and /host/run bind mounts, and the broker
// container mounts it read-only at the same path so `rpk redpanda
// start` picks the cpuset up without any extra flag. /var/run is tmpfs,
// so state never outlives a node reboot.
const HostTunerStateFilePath = "/var/run/redpanda_node_tuner_state.yaml"

func statefulSetInitContainerTuning(state *RenderState) *corev1.Container {
	if !state.Values.Tuning.TuneAIOEvents {
		return nil
	}

	if state.Values.Tuning.ApplyHostTuners {
		return statefulSetInitContainerTuningOnHost(state)
	}

	return &corev1.Container{
		Name:  RedpandaTuningContainerName,
		Image: fmt.Sprintf("%s:%s", state.Values.Image.Repository, Tag(state)),
		Command: []string{
			`/bin/bash`,
			`-c`,
			`rpk redpanda tune all`,
		},
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
			CommonMounts(state),
			corev1.VolumeMount{
				Name:      "base-config",
				MountPath: "/etc/redpanda",
			},
			corev1.VolumeMount{
				Name:      `datadir`,
				MountPath: `/var/lib/redpanda/data`,
			},
		),
	}
}

// statefulSetInitContainerTuningOnHost returns the tuning init container
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
func statefulSetInitContainerTuningOnHost(state *RenderState) *corev1.Container {
	return &corev1.Container{
		Name:    RedpandaTuningContainerName,
		Image:   fmt.Sprintf("%s:%s", state.Values.Image.Repository, Tag(state)),
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

// HostTunerVolumeMounts returns the volume mounts for the host-mode
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
func HostTunerVolumeMounts() []corev1.VolumeMount {
	readOnlyDirs := map[string]bool{
		"bin":   true,
		"sbin":  true,
		"usr":   true,
		"lib":   true,
		"lib64": true,
	}
	mounts := []corev1.VolumeMount{}
	for _, dir := range HostTunerDirs() {
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
		MountPath: HostTunerStateFilePath,
		ReadOnly:  true,
	}
}

// HostTunerScript returns the bash script run by the host-mode tuning
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
func HostTunerScript() string {
	return `set -xeuo pipefail
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
}

func statefulSetInitContainerSetDataDirOwnership(state *RenderState, pool Pool) *corev1.Container {
	if !pool.Statefulset.InitContainers.SetDataDirOwnership.Enabled {
		return nil
	}

	uid, gid := securityContextUidGid(state, pool, "set-datadir-ownership")

	return &corev1.Container{
		Name:  SetDataDirectoryOwnershipContainerName,
		Image: fmt.Sprintf("%s:%s", pool.Statefulset.InitContainerImage.Repository, pool.Statefulset.InitContainerImage.Tag),
		Command: []string{
			`/bin/sh`,
			`-c`,
			fmt.Sprintf(`chown %d:%d -R /var/lib/redpanda/data`, uid, gid),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: append(
			CommonMounts(state),
			corev1.VolumeMount{
				Name:      `datadir`,
				MountPath: `/var/lib/redpanda/data`,
			},
		),
	}
}

//nolint:stylecheck
func securityContextUidGid(state *RenderState, pool Pool, containerName string) (int64, int64) {
	gid, uid := giduidFromPodTemplate(&state.Values.PodTemplate, RedpandaContainerName)
	sgid, suid := giduidFromPodTemplate(&pool.Statefulset.PodTemplate, RedpandaContainerName)

	if sgid != nil {
		gid = sgid
	}

	if suid != nil {
		uid = suid
	}

	if gid == nil {
		panic(fmt.Sprintf(`%s container requires runAsUser to be specified`, containerName))
	}

	if uid == nil {
		panic(fmt.Sprintf(`%s container requires fsGroup to be specified`, containerName))
	}

	return *uid, *gid
}

func giduidFromPodTemplate(tpl *PodTemplate, containerName string) (*int64, *int64) {
	var gid *int64
	var uid *int64

	if tpl.Spec == nil {
		return nil, nil
	}

	if tpl.Spec.SecurityContext != nil {
		gid = tpl.Spec.SecurityContext.FSGroup
		uid = tpl.Spec.SecurityContext.RunAsUser
	}

	for _, container := range tpl.Spec.Containers {
		if ptr.Deref(container.Name, "") == containerName && container.SecurityContext != nil {
			if container.SecurityContext.RunAsUser != nil {
				uid = container.SecurityContext.RunAsUser
			}
		}
	}

	return gid, uid
}

func statefulSetInitContainerFSValidator(state *RenderState, pool Pool) *corev1.Container {
	if !pool.Statefulset.InitContainers.FSValidator.Enabled {
		return nil
	}

	return &corev1.Container{
		Name:    FSValidatorContainerName,
		Image:   fmt.Sprintf("%s:%s", state.Values.Image.Repository, Tag(state)),
		Command: []string{`/bin/sh`},
		Args: []string{
			`-c`,
			fmt.Sprintf(`trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!`,
				pool.Statefulset.InitContainers.FSValidator.ExpectedFS,
			),
		},
		VolumeMounts: append(
			CommonMounts(state),
			corev1.VolumeMount{
				Name:      fmt.Sprintf(`%.49s-fs-validator`, Fullname(state)),
				MountPath: `/etc/secrets/fs-validator/scripts/`,
			},
			corev1.VolumeMount{
				Name:      `datadir`,
				MountPath: `/var/lib/redpanda/data`,
			},
		),
	}
}

func statefulSetInitContainerSetTieredStorageCacheDirOwnership(state *RenderState, pool Pool) *corev1.Container {
	if !state.Values.Storage.IsTieredStorageEnabled() {
		return nil
	}

	uid, gid := securityContextUidGid(state, pool, "set-tiered-storage-cache-dir-ownership")
	cacheDir := state.Values.Storage.TieredCacheDirectory(state)
	mounts := CommonMounts(state)
	mounts = append(mounts, corev1.VolumeMount{
		Name:      "datadir",
		MountPath: "/var/lib/redpanda/data",
	})
	if state.Values.Storage.TieredMountType() != "none" {
		name := "tiered-storage-dir"
		if state.Values.Storage.PersistentVolume != nil && state.Values.Storage.PersistentVolume.NameOverwrite != "" {
			name = state.Values.Storage.PersistentVolume.NameOverwrite
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name:      name,
			MountPath: cacheDir,
		})
	}

	return &corev1.Container{
		Name:  SetTieredStorageCacheOwnershipContainerName,
		Image: fmt.Sprintf(`%s:%s`, pool.Statefulset.InitContainerImage.Repository, pool.Statefulset.InitContainerImage.Tag),
		Command: []string{
			`/bin/sh`,
			`-c`,
			fmt.Sprintf(`mkdir -p %s; chown %d:%d -R %s`,
				cacheDir,
				uid, gid,
				cacheDir,
			),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To[int64](0),
			RunAsGroup: ptr.To[int64](0),
		},
		VolumeMounts: mounts,
	}
}

func statefulSetInitContainerConfigurator(state *RenderState) *corev1.Container {
	volMounts := CommonMounts(state)
	volMounts = append(volMounts,
		corev1.VolumeMount{
			Name:      "config",
			MountPath: "/etc/redpanda",
		},
		corev1.VolumeMount{
			Name:      "base-config",
			MountPath: "/tmp/base-config",
		},
		corev1.VolumeMount{
			Name:      fmt.Sprintf(`%.51s-configurator`, Fullname(state)),
			MountPath: "/etc/secrets/configurator/scripts/",
		},
	)

	if state.Values.RackAwareness.Enabled {
		volMounts = append(volMounts, corev1.VolumeMount{
			Name:      ServiceAccountVolumeName,
			MountPath: DefaultAPITokenMountPath,
			ReadOnly:  true,
		})
	}

	return &corev1.Container{
		Name:  RedpandaConfiguratorContainerName,
		Image: fmt.Sprintf(`%s:%s`, state.Values.Image.Repository, Tag(state)),
		Command: []string{
			`/bin/bash`,
			`-c`,
			`trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`,
		},
		Env: rpkEnvVars(state, []corev1.EnvVar{
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
		}),
		VolumeMounts: volMounts,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
	}
}

func StatefulSetContainers(state *RenderState, pool Pool) []corev1.Container {
	var containers []corev1.Container
	containers = append(containers, statefulSetContainerRedpanda(state, pool))
	if c := statefulSetContainerSidecar(state, pool); c != nil {
		containers = append(containers, *c)
	}
	return containers
}

// wrapLifecycleHook wraps the given command in an attempt to make it more friendly for Kubernetes' lifecycle hooks.
//   - It attaches a maximum time limit by wrapping the command with `timeout -v <timeout>`
//   - It redirect stderr to stdout so all logs from cmd get the same treatment.
//   - It prepends the "lifecycle-hook $(hook) $(date)" to al lines emitted by the hook for easy identification.
//   - It tees the output to fd 1 of pid 1 so it shows up in kubectl logs.
//   - When the wrapped command exceeds the time budget, it emits a clearly-marked
//     TIMEOUT line — `timeout`'s own message can be sparse, and the trailing
//     `true` below previously made the failure invisible to anyone scanning pod
//     logs for a reason the broker shut down ungracefully. PIPESTATUS[0] is
//     read for timeout's exit code (124 = SIGTERM sent, 137 = SIGKILL).
//   - It still terminates the entire command with "true" so non-zero exits
//     don't poison container lifecycle on transient hook issues. The TIMEOUT
//     marker above is the diagnostic signal operators grep for.
func wrapLifecycleHook(hook string, timeoutSeconds int64, cmd []string) []string {
	wrapped := helmette.Join(" ", cmd)
	script := fmt.Sprintf(
		`timeout -v %d %s 2>&1 | sed "s/^/lifecycle-hook %s $(date): /" | tee /proc/1/fd/1`+"\n"+
			`ec=${PIPESTATUS[0]}`+"\n"+
			`if [ "$ec" = "124" ] || [ "$ec" = "137" ]; then`+"\n"+
			`  echo "lifecycle-hook %s $(date): TIMEOUT after %ds — hook killed before completion; the broker will receive SIGTERM with work in-flight (exit $ec)" | tee /proc/1/fd/1`+"\n"+
			`fi`+"\n"+
			`true`,
		timeoutSeconds, wrapped, hook, hook, timeoutSeconds,
	)
	return []string{"bash", "-c", script}
}

func statefulSetContainerRedpanda(state *RenderState, pool Pool) corev1.Container {
	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "$(SERVICE_NAME)", InternalDomain(state))

	container := corev1.Container{
		Name:  RedpandaContainerName,
		Image: fmt.Sprintf(`%s:%s`, state.Values.Image.Repository, Tag(state)),
		Env:   append(bootstrapEnvVars(state, statefulSetRedpandaEnv()), MetricsEnvironmentVariables(state, pool)...),
		Lifecycle: &corev1.Lifecycle{
			// finish the lifecycle scripts with "true" to prevent them from terminating the pod prematurely
			PostStart: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: wrapLifecycleHook(
						"post-start",
						*pool.Statefulset.PodTemplate.Spec.TerminationGracePeriodSeconds/2,
						[]string{"bash", "-x", "/var/lifecycle/postStart.sh"},
					),
				},
			},
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: wrapLifecycleHook(
						"pre-stop",
						*pool.Statefulset.PodTemplate.Spec.TerminationGracePeriodSeconds/2,
						[]string{"bash", "-x", "/var/lifecycle/preStop.sh"},
					),
				},
			},
		},
		StartupProbe: &corev1.Probe{
			// the startupProbe checks to see that the admin api is listening and that the broker has a node_id assigned. This
			// check is only used to delay the start of the liveness and readiness probes until it passes.
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						`/bin/sh`,
						`-c`,
						helmette.Join("\n", []string{
							`set -e`,
							fmt.Sprintf(`RESULT=$(curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready")`,
								adminTLSCurlFlags(state),
								adminInternalHTTPProtocol(state),
								adminApiURLs(state),
							),
							`echo $RESULT`,
							`echo $RESULT | grep ready`,
							``,
						}),
					},
				},
			},
			FailureThreshold:    120,
			InitialDelaySeconds: 1,
			PeriodSeconds:       10,
		},
		LivenessProbe: &corev1.Probe{
			// the livenessProbe just checks to see that the admin api is listening.
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(state.Values.Listeners.Admin.Port),
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
		},
		Command: []string{
			`rpk`,
			`redpanda`,
			`start`,
			fmt.Sprintf(`--advertise-rpc-addr=%s:%d`,
				internalAdvertiseAddress,
				state.Values.Listeners.RPC.Port,
			),
		},
		VolumeMounts: StatefulSetVolumeMounts(state),
		Resources:    state.Values.Resources.GetResourceRequirements(),
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
	}

	// admin http kafka schemaRegistry rpc
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "admin",
		ContainerPort: state.Values.Listeners.Admin.Port,
	})
	for externalName, external := range helmette.SortedMap(state.Values.Listeners.Admin.External) {
		if external.IsEnabled() {
			// The original template used
			// $external.port > 0 &&
			// [ $external.enabled ||
			//   (state.Values.External.Enabled && (dig "enabled" true $external)
			// ]
			// ... which is equivalent to the above check
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          fmt.Sprintf("admin-%.8s", helmette.Lower(externalName)),
				ContainerPort: external.Port,
			})
		}
	}
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "http",
		ContainerPort: state.Values.Listeners.HTTP.Port,
	})
	for externalName, external := range helmette.SortedMap(state.Values.Listeners.HTTP.External) {
		if external.IsEnabled() {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          fmt.Sprintf("http-%.8s", helmette.Lower(externalName)),
				ContainerPort: external.Port,
			})
		}
	}
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "kafka",
		ContainerPort: state.Values.Listeners.Kafka.Port,
	})
	for externalName, external := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		if external.IsEnabled() {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          fmt.Sprintf("kafka-%.8s", helmette.Lower(externalName)),
				ContainerPort: external.Port,
			})
		}
	}
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "rpc",
		ContainerPort: state.Values.Listeners.RPC.Port,
	})
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "schemaregistry",
		ContainerPort: state.Values.Listeners.SchemaRegistry.Port,
	})
	for externalName, external := range helmette.SortedMap(state.Values.Listeners.SchemaRegistry.External) {
		if external.IsEnabled() {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          fmt.Sprintf("schema-%.8s", helmette.Lower(externalName)),
				ContainerPort: external.Port,
			})
		}
	}

	if state.Values.Storage.IsTieredStorageEnabled() && state.Values.Storage.TieredMountType() != "none" {
		name := "tiered-storage-dir"
		if state.Values.Storage.PersistentVolume != nil && state.Values.Storage.PersistentVolume.NameOverwrite != "" {
			name = state.Values.Storage.PersistentVolume.NameOverwrite
		}
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{
				Name:      name,
				MountPath: state.Values.Storage.TieredCacheDirectory(state),
			},
		)
	}

	return container
}

// adminApiURLs was: admin-api-urls
//
//nolint:stylecheck
func adminApiURLs(state *RenderState) string {
	return fmt.Sprintf(`${SERVICE_NAME}.%s:%d`,
		InternalDomain(state),
		state.Values.Listeners.Admin.Port,
	)
}

//nolint:stylecheck
func adminURLsCLI(state *RenderState) string {
	return fmt.Sprintf(`$(SERVICE_NAME).%s:%d`,
		InternalDomain(state),
		state.Values.Listeners.Admin.Port,
	)
}

func statefulSetContainerSidecar(state *RenderState, pool Pool) *corev1.Container {
	args := []string{
		`/redpanda-operator`,
		`sidecar`,
		`--redpanda-yaml`,
		`/etc/redpanda/redpanda.yaml`,
		`--redpanda-cluster-namespace`,
		state.Release.Namespace,
		`--redpanda-cluster-name`,
		Fullname(state),
		// Values pulled from FullLabels.
		fmt.Sprintf(
			"--selector=helm.sh/chart=%s,app.kubernetes.io/name=%s,app.kubernetes.io/instance=%s",
			ChartLabel(state),
			Name(state),
			state.Dot.Release.Name,
		),
		`--run-broker-probe`,
		`--broker-probe-broker-url`,
		// even though this is named "...URLs", it returns
		// only the url for the given pod
		adminURLsCLI(state),
	}

	// Keep the rpk stanza of redpanda.yaml in sync with the chart-rendered
	// base-config (a ConfigMap mount the kubelet keeps up to date), so in-pod
	// rpk tracks topology changes without a pod restart (K8S-755). Seed-server
	// changes deliberately do not roll pods, so this is the only restart-free
	// refresh path. On by default; statefulset.sideCars.rpkProfileWatcher.enabled
	// is the kill switch.
	if pool.Statefulset.SideCars.RPKProfileWatcher.Enabled {
		args = append(args, []string{
			`--watch-rpk-profile`,
			`--rpk-profile-source=/tmp/base-config/redpanda.yaml`,
		}...)
	}

	if pool.Statefulset.SideCars.BrokerDecommissioner.Enabled {
		args = append(args, []string{
			`--run-decommissioner`,
			fmt.Sprintf("--decommission-vote-interval=%s", pool.Statefulset.SideCars.BrokerDecommissioner.DecommissionAfter),
			fmt.Sprintf("--decommission-requeue-timeout=%s", pool.Statefulset.SideCars.BrokerDecommissioner.DecommissionRequeueTimeout),
			`--decommission-vote-count=2`,
		}...)
	}

	if sasl := state.Values.Auth.SASL; sasl.Enabled && sasl.SecretRef != "" && pool.Statefulset.SideCars.ConfigWatcher.Enabled {
		args = append(args, []string{
			`--watch-users`,
			`--users-directory=/etc/secrets/users/`,
		}...)
	}

	if pool.Statefulset.SideCars.PVCUnbinder.Enabled {
		args = append(args, []string{
			`--run-pvc-unbinder`,
			fmt.Sprintf("--pvc-unbinder-timeout=%s", pool.Statefulset.SideCars.PVCUnbinder.UnbindAfter),
		}...)

		if pool.Statefulset.SideCars.PVCUnbinder.DisableStuckClaimExemption {
			args = append(args, `--disable-pvc-rebinding-gate-exemption`)
		}
	}

	args = append(args, pool.Statefulset.SideCars.Args...)

	volumeMounts := append(
		CommonMounts(state),
		corev1.VolumeMount{
			Name:      "config",
			MountPath: "/etc/redpanda",
		},
		// base-config is the chart-rendered ConfigMap; the kubelet keeps
		// this mount up to date in running pods, which is what lets
		// --watch-rpk-profile refresh the rpk stanza without a restart.
		corev1.VolumeMount{
			Name:      "base-config",
			MountPath: "/tmp/base-config",
			ReadOnly:  true,
		},
		corev1.VolumeMount{
			Name:      ServiceAccountVolumeName,
			MountPath: DefaultAPITokenMountPath,
			ReadOnly:  true,
		},
	)

	// Pin the sidecar to redpanda's effective uid/gid. --watch-rpk-profile
	// rewrites /etc/redpanda/redpanda.yaml in place (new inode via rename,
	// mode 0644) and in-pod rpk — running as the redpanda uid — rewrites and
	// chowns that file on load, so a foreign owner would break every rpk call
	// including the readiness probe. securityContextUidGid resolves the
	// redpanda container's uid, so this stays correct even when a user
	// overrides only redpanda's uid (without a pod-level runAsUser). Same
	// reason the v1 rpk-profile-updater pins RunAsUser. (K8S-755)
	uid, gid := securityContextUidGid(state, pool, SidecarContainerName)

	return &corev1.Container{
		Name:         SidecarContainerName,
		Image:        fmt.Sprintf(`%s:%s`, pool.Statefulset.SideCars.Image.Repository, pool.Statefulset.SideCars.Image.Tag),
		Command:      []string{`/redpanda-operator`},
		Args:         append([]string{`supervisor`, `--`}, args...),
		Env:          append(rpkEnvVars(state, nil), statefulSetRedpandaEnv()...),
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(uid),
			RunAsGroup:               ptr.To(gid),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					// This endpoint is the endpoint for the modified health probe initialized by
					// the sidecar. When it's hit it performs an authenticated request to the cluster
					// and ensures that the broker is in a "healthy" state, i.e. it doesn't have
					// under-replicated partitions, it's part of the cluster quorum, etc.
					Path: "/healthz",
					Port: intstr.FromInt32(8093),
				},
			},
			FailureThreshold:    3,
			InitialDelaySeconds: 1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			TimeoutSeconds:      0,
		},
	}
}

func rpkEnvVars(state *RenderState, envVars []corev1.EnvVar) []corev1.EnvVar {
	if state.Values.Auth.SASL != nil && state.Values.Auth.SASL.Enabled {
		return append(envVars, state.Values.Auth.SASL.BootstrapUser.RpkEnvironment(Fullname(state))...)
	}
	return envVars
}

func bootstrapEnvVars(state *RenderState, envVars []corev1.EnvVar) []corev1.EnvVar {
	if state.Values.Auth.SASL != nil && state.Values.Auth.SASL.Enabled {
		return append(envVars, state.Values.Auth.SASL.BootstrapUser.BootstrapEnvironment(Fullname(state))...)
	}
	return envVars
}

func StatefulSets(state *RenderState) []*appsv1.StatefulSet {
	// default statefulset
	sets := []*appsv1.StatefulSet{StatefulSet(state, Pool{Statefulset: state.Values.Statefulset})}
	for _, set := range state.Pools {
		sets = append(sets, StatefulSet(state, set))
	}
	return sets
}

func StatefulSet(state *RenderState, pool Pool) *appsv1.StatefulSet {
	poolLabels := map[string]string{}
	if pool.Name != "" {
		poolLabels[NodePoolLabelName] = pool.Name
		poolLabels[NodePoolLabelGeneration] = pool.Generation
	}

	set := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s%s", Fullname(state), pool.Suffix()),
			Namespace: state.Release.Namespace,
			Labels: helmette.Merge(map[string]string{
				"app.kubernetes.io/component": fmt.Sprintf("%s%s", Name(state), pool.Suffix()),
			}, poolLabels, FullLabels(state)),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: StatefulSetPodLabelsSelector(state, pool),
			},
			ServiceName:                          ServiceName(state),
			Replicas:                             ptr.To(pool.Statefulset.Replicas),
			UpdateStrategy:                       pool.Statefulset.UpdateStrategy,
			PersistentVolumeClaimRetentionPolicy: pool.Statefulset.PersistentVolumeClaimRetentionPolicy,
			PodManagementPolicy:                  "Parallel",
			Template: StrategicMergePatch(
				StructuredTpl(state, pool.Statefulset.PodTemplate),
				StrategicMergePatch(
					StructuredTpl(state, state.Values.PodTemplate),
					corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: StatefulSetPodLabels(state, pool),
							Annotations: map[string]string{
								"config.redpanda.com/checksum": statefulSetChecksumAnnotation(state, pool),
							},
						},
						Spec: corev1.PodSpec{
							AutomountServiceAccountToken: ptr.To(false),
							ServiceAccountName:           ServiceAccountName(state),
							InitContainers:               StatefulSetInitContainers(state, pool),
							Containers:                   StatefulSetContainers(state, pool),
							Volumes:                      StatefulSetVolumes(state, pool),
						},
					},
				),
			),
			VolumeClaimTemplates: nil, // Set below
		},
	}

	// VolumeClaimTemplates
	if state.Values.Storage.PersistentVolume.Enabled || (state.Values.Storage.IsTieredStorageEnabled() && state.Values.Storage.TieredMountType() == "persistentVolume") {
		if t := volumeClaimTemplateDatadir(state); t != nil {
			set.Spec.VolumeClaimTemplates = append(set.Spec.VolumeClaimTemplates, *t)
		}
		if t := volumeClaimTemplateTieredStorageDir(state); t != nil {
			set.Spec.VolumeClaimTemplates = append(set.Spec.VolumeClaimTemplates, *t)
		}
	}

	return set
}

func semver(state *RenderState) string {
	return strings.TrimPrefix(Tag(state), "v")
}

// statefulSetChecksumAnnotation was statefulset-checksum-annotation
// statefulset-checksum-annotation calculates a checksum that is used
// as the value for the annotation, "checksum/config". When this value
// changes, kube-controller-manager will roll the pods.
//
// Append any additional dependencies that require the pods to restart
// to the $dependencies list.
func statefulSetChecksumAnnotation(state *RenderState, pool Pool) string {
	var dependencies []any
	// NB: Seed servers is excluded to avoid a rolling restart when only
	// replicas is changed.
	dependencies = append(dependencies, RedpandaConfigFile(state, false, pool))
	if state.Values.External.Enabled {
		dependencies = append(dependencies, ptr.Deref(state.Values.External.Domain, ""))
		if helmette.Empty(state.Values.External.Addresses) {
			dependencies = append(dependencies, "")
		} else {
			dependencies = append(dependencies, state.Values.External.Addresses)
		}
	}
	return helmette.Sha256Sum(helmette.ToJSON(dependencies))
}

func volumeClaimTemplateDatadir(state *RenderState) *corev1.PersistentVolumeClaim {
	if !state.Values.Storage.PersistentVolume.Enabled {
		return nil
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "datadir",
			Labels: helmette.Merge(map[string]string{
				`app.kubernetes.io/name`:      Name(state),
				`app.kubernetes.io/instance`:  state.Release.Name,
				`app.kubernetes.io/component`: Name(state),
			},
				state.Values.Storage.PersistentVolume.Labels,
				state.Values.CommonLabels,
			),
			Annotations: helmette.Default(nil, state.Values.Storage.PersistentVolume.Annotations),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: helmette.UnmarshalInto[corev1.ResourceList](map[string]any{
					"storage": state.Values.Storage.PersistentVolume.Size,
				}),
			},
		},
	}

	if !helmette.Empty(state.Values.Storage.PersistentVolume.StorageClass) {
		if state.Values.Storage.PersistentVolume.StorageClass == "-" {
			pvc.Spec.StorageClassName = ptr.To("")
		} else {
			pvc.Spec.StorageClassName = ptr.To(state.Values.Storage.PersistentVolume.StorageClass)
		}
	}

	return pvc
}

func volumeClaimTemplateTieredStorageDir(state *RenderState) *corev1.PersistentVolumeClaim {
	if !state.Values.Storage.IsTieredStorageEnabled() || state.Values.Storage.TieredMountType() != "persistentVolume" {
		return nil
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: helmette.Default("tiered-storage-dir", state.Values.Storage.PersistentVolume.NameOverwrite),
			Labels: helmette.Merge(map[string]string{
				`app.kubernetes.io/name`:      Name(state),
				`app.kubernetes.io/instance`:  state.Release.Name,
				`app.kubernetes.io/component`: Name(state),
			},
				state.Values.Storage.TieredPersistentVolumeLabels(),
				state.Values.CommonLabels,
			),
			Annotations: helmette.Default(nil, state.Values.Storage.TieredPersistentVolumeAnnotations()),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: helmette.UnmarshalInto[corev1.ResourceList](map[string]any{
					"storage": state.Values.Storage.GetTieredStorageConfig()[`cloud_storage_cache_size`],
				}),
			},
		},
	}

	if sc := state.Values.Storage.TieredPersistentVolumeStorageClass(); sc == "-" {
		pvc.Spec.StorageClassName = ptr.To("")
	} else if !helmette.Empty(sc) {
		pvc.Spec.StorageClassName = ptr.To(sc)
	}

	return pvc
}

// StorageTieredConfig was: storage-tiered-config
// Wrap this up since there are helm tests that require it
func StorageTieredConfig(state *RenderState) map[string]any {
	return state.Values.Storage.GetTieredStorageConfig()
}
