{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/statefulset_init.go" */ -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.Render" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $containers := (coalesce nil) -}}
{{- if (ne (toJson $r.Tuning) "null") -}}
{{- if $r.Tuning.OnHost -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.tuningOnHostContainer" (dict "a" (list (deepCopy $r))))) "r"))) -}}
{{- else -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.tuningContainer" (dict "a" (list (deepCopy $r))))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if (ne (toJson $r.DataDirOwnership) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.setDataDirOwnershipContainer" (dict "a" (list (deepCopy $r) $r.DataDirOwnership)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.FSValidator) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.fsValidatorContainer" (dict "a" (list (deepCopy $r) $r.FSValidator)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.TieredStorageCacheOwnership) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.setTieredStorageCacheDirOwnershipContainer" (dict "a" (list (deepCopy $r) $r.TieredStorageCacheOwnership)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.Configurator) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.configuratorContainer" (dict "a" (list (deepCopy $r) $r.Configurator)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.Bootstrap) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.bootstrapYamlTemplaterContainer" (dict "a" (list (deepCopy $r) $r.Bootstrap)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $containers) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.mounts" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mounts := (coalesce nil) -}}
{{- $mounts = (concat (default (list) $mounts) (default (list) $r.CommonMounts)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $mounts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.setDataDirOwnershipContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-datadir-ownership" "image" $r.InitImage "command" (list `/bin/sh` `-c` (printf `chown %d:%d -R %s` ($opts.UID | int64) ($opts.GID | int64) "/var/lib/redpanda/data")) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.fsValidatorContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "fs-validator" "image" $r.Image "command" (list `/bin/sh`) "args" (list `-c` (printf `trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!` $opts.ExpectedFS)) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "fs-validator" "mountPath" `/etc/secrets/fs-validator/scripts/`)) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.configuratorContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volMounts := (concat (default (list) (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/tmp/base-config")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "configurator" "mountPath" "/etc/secrets/configurator/scripts/")))) -}}
{{- if $opts.MountAPIToken -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "kube-api-access" "mountPath" "/var/run/secrets/kubernetes.io/serviceaccount" "readOnly" true)))) -}}
{{- end -}}
{{- $env := (list (mustMergeOverwrite (dict "name" "") (dict "name" "CONFIGURATOR_SCRIPT" "value" "/etc/secrets/configurator/scripts/configurator.sh")) (mustMergeOverwrite (dict "name" "") (dict "name" "SERVICE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "metadata.name")) "resourceFieldRef" (coalesce nil) "configMapKeyRef" (coalesce nil) "secretKeyRef" (coalesce nil))))) (mustMergeOverwrite (dict "name" "") (dict "name" "KUBERNETES_NODE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "spec.nodeName")))))) (mustMergeOverwrite (dict "name" "") (dict "name" "HOST_IP_ADDRESS" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "apiVersion" "v1" "fieldPath" "status.hostIP"))))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "redpanda-configurator" "image" $r.Image "command" (list `/bin/bash` `-c` `trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`) "env" (concat (default (list) $env) (default (list) $opts.AdditionalEnv)) "volumeMounts" $volMounts "securityContext" (mustMergeOverwrite (dict) (dict "runAsNonRoot" true "allowPrivilegeEscalation" false))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.setTieredStorageCacheDirOwnershipContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volMounts := (concat (default (list) (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data")))) -}}
{{- if (ne $opts.CacheVolumeName "") -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $opts.CacheVolumeName "mountPath" $opts.CacheDirectory)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-tiered-storage-cache-dir-ownership" "image" $r.InitImage "command" (list `/bin/sh` `-c` (printf `mkdir -p %s; chown %d:%d -R %s` $opts.CacheDirectory ($opts.UID | int64) ($opts.GID | int64) $opts.CacheDirectory)) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" $volMounts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.bootstrapYamlTemplaterContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "bootstrap-yaml-envsubst" "image" $r.SidecarImage "command" (concat (default (list) (list "/redpanda-operator" "bootstrap" "--in-dir" "/tmp/base-config" "--out-dir" "/tmp/config")) (default (list) $opts.AdditionalCLIArgs)) "env" $opts.Env "resources" (mustMergeOverwrite (dict) (dict "limits" (dict "cpu" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "100m")))) "r") "memory" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "125Mi")))) "r")) "requests" (dict "cpu" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "100m")))) "r") "memory" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "125Mi")))) "r")))) "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false "readOnlyRootFilesystem" true "runAsNonRoot" true)) "volumeMounts" (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/tmp/config/")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" (printf "%s%s" "/tmp/base-config" "/"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.tuningContainer" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "tuning" "image" $r.Image "command" (list `/bin/bash` `-c` `rpk redpanda tune all`) "securityContext" (mustMergeOverwrite (dict) (dict "capabilities" (mustMergeOverwrite (dict) (dict "add" (list `SYS_RESOURCE`))) "privileged" true "runAsNonRoot" false "runAsUser" ((0 | int64) | int64) "runAsGroup" ((0 | int64) | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainerRenderer.tuningOnHostContainer" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "tuning" "image" $r.Image "command" (list `/bin/bash` `-c` "set -xeuo pipefail\numask 077\nmkdir -p /host/opt/redpanda\nmount --bind /opt/redpanda /host/opt/redpanda\nprintf '#!/bin/sh\\ncommand -v \"$@\"\\n' > /opt/redpanda/bin/which\nchmod +x /opt/redpanda/bin/which\nchroot /host /bin/bash -c 'true' || { echo \"FATAL: cannot exec /bin/bash inside the /host chroot; this node's filesystem layout is not supported by tuning.apply_host_tuners\" >&2; exit 1; }\ntrap 'rm -f /host/var/tmp/redpanda-tune.yaml' EXIT\ncp /host/redpanda_etc/redpanda.yaml /host/var/tmp/redpanda-tune.yaml\ngrep -q 'data_directory:' /host/var/tmp/redpanda-tune.yaml || sed -i 's|^redpanda:|redpanda:\\n  data_directory: /var/lib/redpanda/data|' /host/var/tmp/redpanda-tune.yaml\nchroot /host /bin/bash -c '\n  set -xeuo pipefail\n  export PATH=\"/opt/redpanda/bin:$PATH\"\n  nsenter -t 1 -n /opt/redpanda/bin/rpk redpanda tune list --config /var/tmp/redpanda-tune.yaml\n  rc=0\n  nsenter -t 1 -n /opt/redpanda/bin/rpk redpanda tune all --config /var/tmp/redpanda-tune.yaml -v || rc=$?\n  if [ \"$rc\" -ne 0 ]; then\n    echo \"WARNING: rpk redpanda tune all exited $rc; at least one enabled tuner failed to apply (see output above). Not blocking broker startup over a single degraded tuner.\" >&2\n  fi\n  busctl call org.freedesktop.systemd1 /org/freedesktop/systemd1 \\\n    org.freedesktop.systemd1.Manager TryRestartUnit ss \"irqbalance.service\" \"replace\" \\\n    || true\n'\n") "securityContext" (mustMergeOverwrite (dict) (dict "privileged" true "runAsNonRoot" false "runAsUser" ((0 | int64) | int64) "runAsGroup" ((0 | int64) | int64))) "volumeMounts" (get (fromJson (include "redpanda.hostTunerVolumeMounts" (dict "a" (list)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.hostTunerDirs" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "bin" "sbin" "sys" "proc" "etc" "usr" "lib" "lib64" "dev" "var" "run")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.hostTunerVolumeMounts" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $readOnlyDirs := (dict "bin" true "sbin" true "usr" true "lib" true "lib64" true) -}}
{{- $mounts := (list) -}}
{{- range $_, $dir := (get (fromJson (include "redpanda.hostTunerDirs" (dict "a" (list)))) "r") -}}
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

{{- define "redpanda.HostTunerStateVolumeMount" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "host-tuner-state" "mountPath" "/var/run/redpanda_node_tuner_state.yaml" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

