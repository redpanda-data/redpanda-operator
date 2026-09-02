{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/statefulset_init.go" */ -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.Render" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $containers := (coalesce nil) -}}
{{- if (ne (toJson $r.Tuning) "null") -}}
{{- if $r.Tuning.OnHost -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.tuningOnHostContainer" (dict "a" (list (deepCopy $r))))) "r"))) -}}
{{- else -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.tuningContainer" (dict "a" (list (deepCopy $r))))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- if (ne (toJson $r.DataDirOwnership) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.setDataDirOwnershipContainer" (dict "a" (list (deepCopy $r) $r.DataDirOwnership)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.FSValidator) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.fsValidatorContainer" (dict "a" (list (deepCopy $r) $r.FSValidator)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.TieredStorageCacheOwnership) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.setTieredStorageCacheDirOwnershipContainer" (dict "a" (list (deepCopy $r) $r.TieredStorageCacheOwnership)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.Configurator) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.configuratorContainer" (dict "a" (list (deepCopy $r) $r.Configurator)))) "r"))) -}}
{{- end -}}
{{- if (ne (toJson $r.Bootstrap) "null") -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.bootstrapYamlTemplaterContainer" (dict "a" (list (deepCopy $r) $r.Bootstrap)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $containers) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.mounts" -}}
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

{{- define "_redpanda.StatefulSetInitContainerRenderer.setDataDirOwnershipContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-datadir-ownership" "image" $r.InitImage "command" (list `/bin/sh` `-c` (printf `chown %d:%d -R %s` ($opts.UID | int64) ($opts.GID | int64) "/var/lib/redpanda/data")) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.fsValidatorContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "fs-validator" "image" $r.Image "command" (list `/bin/sh`) "args" (list `-c` (printf `trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!` $opts.ExpectedFS)) "volumeMounts" (concat (default (list) (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "fs-validator" "mountPath" `/etc/secrets/fs-validator/scripts/`)) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.configuratorContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volMounts := (concat (default (list) (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/tmp/base-config")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "configurator" "mountPath" "/etc/secrets/configurator/scripts/")))) -}}
{{- if $opts.MountAPIToken -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "kube-api-access" "mountPath" "/var/run/secrets/kubernetes.io/serviceaccount" "readOnly" true)))) -}}
{{- end -}}
{{- $env := (list (mustMergeOverwrite (dict "name" "") (dict "name" "CONFIGURATOR_SCRIPT" "value" "/etc/secrets/configurator/scripts/configurator.sh")) (mustMergeOverwrite (dict "name" "") (dict "name" "SERVICE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "metadata.name")) "resourceFieldRef" (coalesce nil) "configMapKeyRef" (coalesce nil) "secretKeyRef" (coalesce nil))))) (mustMergeOverwrite (dict "name" "") (dict "name" "KUBERNETES_NODE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "spec.nodeName")))))) (mustMergeOverwrite (dict "name" "") (dict "name" "HOST_IP_ADDRESS" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "apiVersion" "v1" "fieldPath" "status.hostIP"))))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "redpanda-configurator" "image" $r.Image "command" (list `/bin/bash` `-c` `trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`) "env" (concat (default (list) $env) (default (list) $opts.AdditionalEnv)) "volumeMounts" $volMounts "securityContext" (mustMergeOverwrite (dict) (dict "runAsNonRoot" true "allowPrivilegeEscalation" false))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.setTieredStorageCacheDirOwnershipContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volMounts := (concat (default (list) (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data")))) -}}
{{- if (ne $opts.CacheVolumeName "") -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $opts.CacheVolumeName "mountPath" $opts.CacheDirectory)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-tiered-storage-cache-dir-ownership" "image" $r.InitImage "command" (list `/bin/sh` `-c` (printf `mkdir -p %s; chown %d:%d -R %s` $opts.CacheDirectory ($opts.UID | int64) ($opts.GID | int64) $opts.CacheDirectory)) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" $volMounts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.bootstrapYamlTemplaterContainer" -}}
{{- $r := (index .a 0) -}}
{{- $opts := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "bootstrap-yaml-envsubst" "image" $r.SidecarImage "command" (concat (default (list) (list "/redpanda-operator" "bootstrap" "--in-dir" "/tmp/base-config" "--out-dir" "/tmp/config")) (default (list) $opts.AdditionalCLIArgs)) "env" $opts.Env "resources" (mustMergeOverwrite (dict) (dict "limits" (dict "cpu" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "100m")))) "r") "memory" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "125Mi")))) "r")) "requests" (dict "cpu" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "100m")))) "r") "memory" (get (fromJson (include "_shims.resource_MustParse" (dict "a" (list "125Mi")))) "r")))) "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false "readOnlyRootFilesystem" true "runAsNonRoot" true)) "volumeMounts" (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/tmp/config/")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" (printf "%s%s" "/tmp/base-config" "/"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.tuningContainer" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "tuning" "image" $r.Image "command" (list `/bin/bash` `-c` `rpk redpanda tune all`) "securityContext" (mustMergeOverwrite (dict) (dict "capabilities" (mustMergeOverwrite (dict) (dict "add" (list `SYS_RESOURCE`))) "privileged" true "runAsNonRoot" false "runAsUser" ((0 | int64) | int64) "runAsGroup" ((0 | int64) | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "_redpanda.StatefulSetInitContainerRenderer.mounts" (dict "a" (list (deepCopy $r))))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.StatefulSetInitContainerRenderer.tuningOnHostContainer" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "tuning" "image" $r.Image "command" (list `/bin/bash` `-c` (get (fromJson (include "_redpanda.HostTunerScript" (dict "a" (list)))) "r")) "securityContext" (mustMergeOverwrite (dict) (dict "privileged" true "runAsNonRoot" false "runAsUser" ((0 | int64) | int64) "runAsGroup" ((0 | int64) | int64))) "volumeMounts" (get (fromJson (include "_redpanda.HostTunerVolumeMounts" (dict "a" (list)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

