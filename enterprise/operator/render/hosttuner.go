// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

// This file duplicates the HostTuner* exports of the OSS Redpanda helm chart
// (github.com/redpanda-data/redpanda-operator/charts/redpanda/v25:
// statefulset.go and values.go). The enterprise module must not import the
// charts module, so these symbols are copied verbatim; the OSS drift-guard
// test (operator/internal/enterprisedrift) pins each copy to its original.

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

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

// HostTunerDefaults returns the per-tuner rpk flags that ApplyHostTuners
// default-enables. The whole point of ApplyHostTuners is to make the rpk
// tuners that need host /sys, /proc, NICs and block devices actually
// fire, and those tuners are gated by per-tuner flags in the rpk section
// of redpanda.yaml, not by ApplyHostTuners itself — so flipping just
// ApplyHostTuners would render the chroot init container running against
// a config where only tune_aio_events is true. See the ApplyHostTuners
// doc comment for per-tuner rationale; the invariant for membership in
// this list is "only works (or only does real work) via the chroot path,
// and cannot crashloop the init container on hosts that lack the
// feature".
//
// These are merged at LOWEST precedence in rpkNodeConfig — after both
// Tuning.Translate() and the user's config.rpk — so an explicit
// `config.rpk.tune_*: false` opt-out always wins over these defaults.
// The multicluster (StretchCluster) renderer applies the same map with
// the same precedence.
func HostTunerDefaults() map[string]any {
	return map[string]any{
		"tune_disk_irq":         true,
		"tune_disk_scheduler":   true,
		"tune_disk_nomerges":    true,
		"tune_network":          true,
		"tune_fstrim":           true,
		"tune_disk_write_cache": true,
		"tune_cpu":              true,
	}
}
