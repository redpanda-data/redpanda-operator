{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/hosttuner.go" */ -}}

{{- define "_redpanda.HostTunerVolumes" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $vols := (list) -}}
{{- range $_, $dir := (get (fromJson (include "_redpanda.HostTunerDirs" (dict "a" (list)))) "r") -}}
{{- $hostPathType := "Directory" -}}
{{- if (eq $dir "lib64") -}}
{{- $hostPathType = "DirectoryOrCreate" -}}
{{- end -}}
{{- $vols = (concat (default (list) $vols) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "hostPath" (mustMergeOverwrite (dict "path" "") (dict "path" (printf "/%s" $dir) "type" $hostPathType)))) (dict "name" (printf "host-%s" $dir))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $vols = (concat (default (list) $vols) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "hostPath" (mustMergeOverwrite (dict "path" "") (dict "path" "/var/run/redpanda_node_tuner_state.yaml" "type" "FileOrCreate")))) (dict "name" "host-tuner-state")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $vols) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerDirs" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "bin" "sbin" "sys" "proc" "etc" "usr" "lib" "lib64" "dev" "var" "run")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerVolumeMounts" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $readOnlyDirs := (dict "bin" true "sbin" true "usr" true "lib" true "lib64" true) -}}
{{- $mounts := (list) -}}
{{- range $_, $dir := (get (fromJson (include "_redpanda.HostTunerDirs" (dict "a" (list)))) "r") -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf "host-%s" $dir) "mountPath" (printf "/host/%s" $dir) "readOnly" (ternary (index $readOnlyDirs $dir) false (hasKey $readOnlyDirs $dir)) "mountPropagation" "HostToContainer")))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/host/redpanda_etc")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/host/var/lib/redpanda/data")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $mounts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerStateVolumeMount" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "host-tuner-state" "mountPath" "/var/run/redpanda_node_tuner_state.yaml" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerScript" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `set -xeuo pipefail
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
`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerDefaults" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "tune_disk_irq" true "tune_disk_scheduler" true "tune_disk_nomerges" true "tune_network" true "tune_fstrim" true "tune_disk_write_cache" true "tune_cpu" true)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

