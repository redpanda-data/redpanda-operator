{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/statefulset.go" */ -}}

{{- define "redpanda.statefulSetRedpandaEnv" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "") (dict "name" "SERVICE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "metadata.name")))))) (mustMergeOverwrite (dict "name" "") (dict "name" "POD_IP" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "status.podIP")))))) (mustMergeOverwrite (dict "name" "") (dict "name" "HOST_IP" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "status.hostIP")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ClusterPodLabelsSelector" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "app.kubernetes.io/instance" $state.Release.Name "app.kubernetes.io/name" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetPodLabelsSelector" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne (toJson $state.StatefulSetSelector) "null") (eq $pool.Name "")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.StatefulSetSelector) | toJson -}}
{{- break -}}
{{- end -}}
{{- $additionalSelectorLabels := (dict) -}}
{{- if (ne (toJson $pool.Statefulset.additionalSelectorLabels) "null") -}}
{{- $additionalSelectorLabels = $pool.Statefulset.additionalSelectorLabels -}}
{{- end -}}
{{- $name := (printf "%s%s" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")) -}}
{{- $component := (printf "%s-statefulset" (trimSuffix "-" (trunc (51 | int) $name))) -}}
{{- $defaults := (dict "app.kubernetes.io/component" $component) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $additionalSelectorLabels $defaults (get (fromJson (include "redpanda.ClusterPodLabelsSelector" (dict "a" (list $state)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetPodLabels" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne (toJson $state.StatefulSetPodLabels) "null") (eq $pool.Name "")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.StatefulSetPodLabels) | toJson -}}
{{- break -}}
{{- end -}}
{{- $statefulSetLabels := (dict) -}}
{{- if (ne (toJson $pool.Statefulset.podTemplate.labels) "null") -}}
{{- $statefulSetLabels = $pool.Statefulset.podTemplate.labels -}}
{{- end -}}
{{- $defaults := (dict "redpanda.com/poddisruptionbudget" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") "cluster.redpanda.com/broker" "true") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $statefulSetLabels (get (fromJson (include "redpanda.StatefulSetPodLabelsSelector" (dict "a" (list $state $pool)))) "r") $defaults (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetVolumes" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fullname := (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") -}}
{{- $poolFullname := (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")) -}}
{{- $volumes := (get (fromJson (include "redpanda.CommonVolumes" (dict "a" (list $state)))) "r") -}}
{{- $volumes = (concat (default (list) $volumes) (default (list) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (printf "%.50s-sts-lifecycle" $fullname) "defaultMode" (0o775 | int))))) (dict "name" "lifecycle-scripts")) (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" $poolFullname)) (dict)))) (dict "name" "base-config")) (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "emptyDir" (mustMergeOverwrite (dict) (dict)))) (dict "name" "config")) (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (printf "%.51s-configurator" $poolFullname) "defaultMode" (0o775 | int))))) (dict "name" (printf "%.51s-configurator" $fullname)))))) -}}
{{- if $pool.Statefulset.initContainers.fsValidator.enabled -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (printf "%.49s-fs-validator" $poolFullname) "defaultMode" (0o775 | int))))) (dict "name" (printf "%.49s-fs-validator" $fullname))))) -}}
{{- end -}}
{{- $vol_1 := (get (fromJson (include "redpanda.Listeners.TrustStoreVolume" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- if (ne (toJson $vol_1) "null") -}}
{{- $volumes = (concat (default (list) $volumes) (list $vol_1)) -}}
{{- end -}}
{{- $volumes = (concat (default (list) $volumes) (list (get (fromJson (include "redpanda.statefulSetVolumeDataDir" (dict "a" (list $state)))) "r"))) -}}
{{- $v_2 := (get (fromJson (include "redpanda.statefulSetVolumeTieredStorageDir" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $v_2) "null") -}}
{{- $volumes = (concat (default (list) $volumes) (list $v_2)) -}}
{{- end -}}
{{- $volumes = (concat (default (list) $volumes) (list (get (fromJson (include "redpanda.kubeTokenAPIVolume" (dict "a" (list "kube-api-access")))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $volumes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.kubeTokenAPIVolume" -}}
{{- $name := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "projected" (mustMergeOverwrite (dict "sources" (coalesce nil)) (dict "defaultMode" (420 | int) "sources" (list (mustMergeOverwrite (dict) (dict "serviceAccountToken" (mustMergeOverwrite (dict "path" "") (dict "path" "token" "expirationSeconds" ((3607 | int) | int64))))) (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" "kube-root-ca.crt")) (dict "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "ca.crt" "path" "ca.crt"))))))) (mustMergeOverwrite (dict) (dict "downwardAPI" (mustMergeOverwrite (dict) (dict "items" (list (mustMergeOverwrite (dict "path" "") (dict "path" "namespace" "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "apiVersion" "v1" "fieldPath" "metadata.namespace")))))))))))))) (dict "name" $name))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetVolumeDataDir" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $datadirSource := (mustMergeOverwrite (dict) (dict "emptyDir" (mustMergeOverwrite (dict) (dict)))) -}}
{{- if $state.Values.storage.persistentVolume.enabled -}}
{{- $datadirSource = (mustMergeOverwrite (dict) (dict "persistentVolumeClaim" (mustMergeOverwrite (dict "claimName" "") (dict "claimName" "datadir")))) -}}
{{- else -}}{{- if (ne $state.Values.storage.hostPath "") -}}
{{- $datadirSource = (mustMergeOverwrite (dict) (dict "hostPath" (mustMergeOverwrite (dict "path" "") (dict "path" $state.Values.storage.hostPath)))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") $datadirSource (dict "name" "datadir"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetVolumeTieredStorageDir" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.Storage.IsTieredStorageEnabled" (dict "a" (list $state.Values.storage)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $tieredType := (get (fromJson (include "redpanda.Storage.TieredMountType" (dict "a" (list $state.Values.storage)))) "r") -}}
{{- if (or (eq $tieredType "none") (eq $tieredType "persistentVolume")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $tieredType "hostPath") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "hostPath" (mustMergeOverwrite (dict "path" "") (dict "path" (get (fromJson (include "redpanda.Storage.GetTieredStorageHostPath" (dict "a" (list $state.Values.storage)))) "r"))))) (dict "name" "tiered-storage-dir"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "emptyDir" (mustMergeOverwrite (dict) (dict "sizeLimit" (get (fromJson (include "redpanda.TieredStorageConfig.CloudStorageCacheSize" (dict "a" (list (deepCopy (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r")))))) "r"))))) (dict "name" "tiered-storage-dir"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetVolumeMounts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mounts := (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r") -}}
{{- $mounts = (concat (default (list) $mounts) (default (list) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/tmp/base-config")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "lifecycle-scripts" "mountPath" "/var/lifecycle")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "kube-api-access" "mountPath" "/var/run/secrets/kubernetes.io/serviceaccount" "readOnly" true))))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list (get (fromJson (include "redpanda.Listeners.TrustStores" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r"))))) "r") | int) (0 | int)) -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "truststores" "mountPath" "/etc/truststores" "readOnly" true)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $mounts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetInitContainers" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $containers := (coalesce nil) -}}
{{- $c_3 := (get (fromJson (include "redpanda.statefulSetInitContainerTuning" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $c_3) "null") -}}
{{- $containers = (concat (default (list) $containers) (list $c_3)) -}}
{{- end -}}
{{- $c_4 := (get (fromJson (include "redpanda.statefulSetInitContainerSetDataDirOwnership" (dict "a" (list $state $pool)))) "r") -}}
{{- if (ne (toJson $c_4) "null") -}}
{{- $containers = (concat (default (list) $containers) (list $c_4)) -}}
{{- end -}}
{{- $c_5 := (get (fromJson (include "redpanda.statefulSetInitContainerFSValidator" (dict "a" (list $state $pool)))) "r") -}}
{{- if (ne (toJson $c_5) "null") -}}
{{- $containers = (concat (default (list) $containers) (list $c_5)) -}}
{{- end -}}
{{- $c_6 := (get (fromJson (include "redpanda.statefulSetInitContainerSetTieredStorageCacheDirOwnership" (dict "a" (list $state $pool)))) "r") -}}
{{- if (ne (toJson $c_6) "null") -}}
{{- $containers = (concat (default (list) $containers) (list $c_6)) -}}
{{- end -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.statefulSetInitContainerConfigurator" (dict "a" (list $state)))) "r"))) -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.bootstrapYamlTemplater" (dict "a" (list $state $pool.Statefulset)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $containers) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetInitContainerTuning" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.tuning.tune_aio_events) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "tuning" "image" (printf "%s:%s" $state.Values.image.repository (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")) "command" (list `/bin/bash` `-c` `rpk redpanda tune all`) "securityContext" (mustMergeOverwrite (dict) (dict "capabilities" (mustMergeOverwrite (dict) (dict "add" (list `SYS_RESOURCE`))) "privileged" true "runAsUser" ((0 | int64) | int64) "runAsGroup" ((0 | int64) | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/etc/redpanda"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetInitContainerSetDataDirOwnership" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $pool.Statefulset.initContainers.setDataDirOwnership.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_397_uid_gid := (get (fromJson (include "redpanda.securityContextUidGid" (dict "a" (list $state $pool "set-datadir-ownership")))) "r") -}}
{{- $uid := ((index $_397_uid_gid 0) | int64) -}}
{{- $gid := ((index $_397_uid_gid 1) | int64) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-datadir-ownership" "image" (printf "%s:%s" $pool.Statefulset.initContainerImage.repository $pool.Statefulset.initContainerImage.tag) "command" (list `/bin/sh` `-c` (printf `chown %d:%d -R /var/lib/redpanda/data` $uid $gid)) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" `datadir` "mountPath" `/var/lib/redpanda/data`))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.securityContextUidGid" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- $containerName := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_423_gid_uid := (get (fromJson (include "redpanda.giduidFromPodTemplate" (dict "a" (list $state.Values.podTemplate "redpanda")))) "r") -}}
{{- $gid := (index $_423_gid_uid 0) -}}
{{- $uid := (index $_423_gid_uid 1) -}}
{{- $_424_sgid_suid := (get (fromJson (include "redpanda.giduidFromPodTemplate" (dict "a" (list $pool.Statefulset.podTemplate "redpanda")))) "r") -}}
{{- $sgid := (index $_424_sgid_suid 0) -}}
{{- $suid := (index $_424_sgid_suid 1) -}}
{{- if (ne (toJson $sgid) "null") -}}
{{- $gid = $sgid -}}
{{- end -}}
{{- if (ne (toJson $suid) "null") -}}
{{- $uid = $suid -}}
{{- end -}}
{{- if (eq (toJson $gid) "null") -}}
{{- $_ := (fail (printf `%s container requires runAsUser to be specified` $containerName)) -}}
{{- end -}}
{{- if (eq (toJson $uid) "null") -}}
{{- $_ := (fail (printf `%s container requires fsGroup to be specified` $containerName)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $uid $gid)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.giduidFromPodTemplate" -}}
{{- $tpl := (index .a 0) -}}
{{- $containerName := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $gid := (coalesce nil) -}}
{{- $uid := (coalesce nil) -}}
{{- if (eq (toJson $tpl.spec) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (coalesce nil) (coalesce nil))) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (ne (toJson $tpl.spec.securityContext) "null") -}}
{{- $gid = $tpl.spec.securityContext.fsGroup -}}
{{- $uid = $tpl.spec.securityContext.runAsUser -}}
{{- end -}}
{{- range $_, $container := $tpl.spec.containers -}}
{{- if (and (eq (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $container.name "")))) "r") $containerName) (ne (toJson $container.securityContext) "null")) -}}
{{- if (ne (toJson $container.securityContext.runAsUser) "null") -}}
{{- $uid = $container.securityContext.runAsUser -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $gid $uid)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetInitContainerFSValidator" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $pool.Statefulset.initContainers.fsValidator.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "fs-validator" "image" (printf "%s:%s" $state.Values.image.repository (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")) "command" (list `/bin/sh`) "args" (list `-c` (printf `trap "exit 0" TERM; exec /etc/secrets/fs-validator/scripts/fsValidator.sh %s & wait $!` $pool.Statefulset.initContainers.fsValidator.expectedFS)) "volumeMounts" (concat (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf `%.49s-fs-validator` (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "mountPath" `/etc/secrets/fs-validator/scripts/`)) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" `datadir` "mountPath" `/var/lib/redpanda/data`))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetInitContainerSetTieredStorageCacheDirOwnership" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.Storage.IsTieredStorageEnabled" (dict "a" (list $state.Values.storage)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_503_uid_gid := (get (fromJson (include "redpanda.securityContextUidGid" (dict "a" (list $state $pool "set-tiered-storage-cache-dir-ownership")))) "r") -}}
{{- $uid := ((index $_503_uid_gid 0) | int64) -}}
{{- $gid := ((index $_503_uid_gid 1) | int64) -}}
{{- $cacheDir := (get (fromJson (include "redpanda.Storage.TieredCacheDirectory" (dict "a" (list $state.Values.storage $state)))) "r") -}}
{{- $mounts := (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r") -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "datadir" "mountPath" "/var/lib/redpanda/data")))) -}}
{{- if (ne (get (fromJson (include "redpanda.Storage.TieredMountType" (dict "a" (list $state.Values.storage)))) "r") "none") -}}
{{- $name := "tiered-storage-dir" -}}
{{- if (and (ne (toJson $state.Values.storage.persistentVolume) "null") (ne $state.Values.storage.persistentVolume.nameOverwrite "")) -}}
{{- $name = $state.Values.storage.persistentVolume.nameOverwrite -}}
{{- end -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $name "mountPath" $cacheDir)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "set-tiered-storage-cache-dir-ownership" "image" (printf `%s:%s` $pool.Statefulset.initContainerImage.repository $pool.Statefulset.initContainerImage.tag) "command" (list `/bin/sh` `-c` (printf `mkdir -p %s; chown %d:%d -R %s` $cacheDir $uid $gid $cacheDir)) "securityContext" (mustMergeOverwrite (dict) (dict "runAsUser" (0 | int64) "runAsGroup" (0 | int64))) "volumeMounts" $mounts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetInitContainerConfigurator" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volMounts := (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r") -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/tmp/base-config")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf `%.51s-configurator` (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "mountPath" "/etc/secrets/configurator/scripts/")))) -}}
{{- if $state.Values.rackAwareness.enabled -}}
{{- $volMounts = (concat (default (list) $volMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "kube-api-access" "mountPath" "/var/run/secrets/kubernetes.io/serviceaccount" "readOnly" true)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "redpanda-configurator" "image" (printf `%s:%s` $state.Values.image.repository (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")) "command" (list `/bin/bash` `-c` `trap "exit 0" TERM; exec $CONFIGURATOR_SCRIPT "${SERVICE_NAME}" "${KUBERNETES_NODE_NAME}" & wait $!`) "env" (get (fromJson (include "redpanda.rpkEnvVars" (dict "a" (list $state (list (mustMergeOverwrite (dict "name" "") (dict "name" "CONFIGURATOR_SCRIPT" "value" "/etc/secrets/configurator/scripts/configurator.sh")) (mustMergeOverwrite (dict "name" "") (dict "name" "SERVICE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "metadata.name")) "resourceFieldRef" (coalesce nil) "configMapKeyRef" (coalesce nil) "secretKeyRef" (coalesce nil))))) (mustMergeOverwrite (dict "name" "") (dict "name" "KUBERNETES_NODE_NAME" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "fieldPath" "spec.nodeName")))))) (mustMergeOverwrite (dict "name" "") (dict "name" "HOST_IP_ADDRESS" "valueFrom" (mustMergeOverwrite (dict) (dict "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "apiVersion" "v1" "fieldPath" "status.hostIP"))))))))))) "r") "volumeMounts" $volMounts))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSetContainers" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $containers := (coalesce nil) -}}
{{- $containers = (concat (default (list) $containers) (list (get (fromJson (include "redpanda.statefulSetContainerRedpanda" (dict "a" (list $state $pool)))) "r"))) -}}
{{- $c_7 := (get (fromJson (include "redpanda.statefulSetContainerSidecar" (dict "a" (list $state $pool)))) "r") -}}
{{- if (ne (toJson $c_7) "null") -}}
{{- $containers = (concat (default (list) $containers) (list $c_7)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $containers) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.wrapLifecycleHook" -}}
{{- $hook := (index .a 0) -}}
{{- $timeoutSeconds := (index .a 1) -}}
{{- $cmd := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $wrapped := (join " " $cmd) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "bash" "-c" (printf "timeout -v %d %s 2>&1 | sed \"s/^/lifecycle-hook %s $(date): /\" | tee /proc/1/fd/1; true" $timeoutSeconds $wrapped $hook))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetContainerRedpanda" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $internalAdvertiseAddress := (printf "%s.%s" "$(SERVICE_NAME)" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) -}}
{{- $container := (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "redpanda" "image" (printf `%s:%s` $state.Values.image.repository (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")) "env" (get (fromJson (include "redpanda.bootstrapEnvVars" (dict "a" (list $state (get (fromJson (include "redpanda.statefulSetRedpandaEnv" (dict "a" (list)))) "r"))))) "r") "lifecycle" (mustMergeOverwrite (dict) (dict "postStart" (mustMergeOverwrite (dict) (dict "exec" (mustMergeOverwrite (dict) (dict "command" (get (fromJson (include "redpanda.wrapLifecycleHook" (dict "a" (list "post-start" ((div $pool.Statefulset.podTemplate.spec.terminationGracePeriodSeconds (2 | int64)) | int64) (list "bash" "-x" "/var/lifecycle/postStart.sh"))))) "r"))))) "preStop" (mustMergeOverwrite (dict) (dict "exec" (mustMergeOverwrite (dict) (dict "command" (get (fromJson (include "redpanda.wrapLifecycleHook" (dict "a" (list "pre-stop" ((div $pool.Statefulset.podTemplate.spec.terminationGracePeriodSeconds (2 | int64)) | int64) (list "bash" "-x" "/var/lifecycle/preStop.sh"))))) "r"))))))) "startupProbe" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "exec" (mustMergeOverwrite (dict) (dict "command" (list `/bin/sh` `-c` (join "\n" (list `set -e` (printf `RESULT=$(curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready")` (get (fromJson (include "redpanda.adminTLSCurlFlags" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.adminInternalHTTPProtocol" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.adminApiURLs" (dict "a" (list $state)))) "r")) `echo $RESULT` `echo $RESULT | grep ready` ``))))))) (dict "failureThreshold" (120 | int) "initialDelaySeconds" (1 | int) "periodSeconds" (10 | int))) "livenessProbe" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "exec" (mustMergeOverwrite (dict) (dict "command" (list `/bin/sh` `-c` (printf `curl --silent --fail -k -m 5 %s "%s://%s/v1/status/ready"` (get (fromJson (include "redpanda.adminTLSCurlFlags" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.adminInternalHTTPProtocol" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.adminApiURLs" (dict "a" (list $state)))) "r"))))))) (dict "failureThreshold" (3 | int) "initialDelaySeconds" (10 | int) "periodSeconds" (10 | int))) "command" (list `rpk` `redpanda` `start` (printf `--advertise-rpc-addr=%s:%d` $internalAdvertiseAddress ($state.Values.listeners.rpc.port | int))) "volumeMounts" (get (fromJson (include "redpanda.StatefulSetVolumeMounts" (dict "a" (list $state)))) "r") "resources" (get (fromJson (include "redpanda.RedpandaResources.GetResourceRequirements" (dict "a" (list $state.Values.resources)))) "r"))) -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "admin" "containerPort" ($state.Values.listeners.admin.port | int)))))) -}}
{{- range $externalName, $external := $state.Values.listeners.admin.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r") -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" (printf "admin-%.8s" (lower $externalName)) "containerPort" ($external.port | int)))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "http" "containerPort" ($state.Values.listeners.http.port | int)))))) -}}
{{- range $externalName, $external := $state.Values.listeners.http.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r") -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" (printf "http-%.8s" (lower $externalName)) "containerPort" ($external.port | int)))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "kafka" "containerPort" ($state.Values.listeners.kafka.port | int)))))) -}}
{{- range $externalName, $external := $state.Values.listeners.kafka.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r") -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" (printf "kafka-%.8s" (lower $externalName)) "containerPort" ($external.port | int)))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "rpc" "containerPort" ($state.Values.listeners.rpc.port | int)))))) -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "schemaregistry" "containerPort" ($state.Values.listeners.schemaRegistry.port | int)))))) -}}
{{- range $externalName, $external := $state.Values.listeners.schemaRegistry.external -}}
{{- if (get (fromJson (include "redpanda.ExternalListener.IsEnabled" (dict "a" (list $external)))) "r") -}}
{{- $_ := (set $container "ports" (concat (default (list) $container.ports) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" (printf "schema-%.8s" (lower $externalName)) "containerPort" ($external.port | int)))))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- if (and (get (fromJson (include "redpanda.Storage.IsTieredStorageEnabled" (dict "a" (list $state.Values.storage)))) "r") (ne (get (fromJson (include "redpanda.Storage.TieredMountType" (dict "a" (list $state.Values.storage)))) "r") "none")) -}}
{{- $name := "tiered-storage-dir" -}}
{{- if (and (ne (toJson $state.Values.storage.persistentVolume) "null") (ne $state.Values.storage.persistentVolume.nameOverwrite "")) -}}
{{- $name = $state.Values.storage.persistentVolume.nameOverwrite -}}
{{- end -}}
{{- $_ := (set $container "volumeMounts" (concat (default (list) $container.volumeMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $name "mountPath" (get (fromJson (include "redpanda.Storage.TieredCacheDirectory" (dict "a" (list $state.Values.storage $state)))) "r")))))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $container) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminApiURLs" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf `${SERVICE_NAME}.%s:%d` (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") ($state.Values.listeners.admin.port | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminURLsCLI" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf `$(SERVICE_NAME).%s:%d` (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") ($state.Values.listeners.admin.port | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetContainerSidecar" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $args := (list `/redpanda-operator` `sidecar` `--redpanda-yaml` `/etc/redpanda/redpanda.yaml` `--redpanda-cluster-namespace` $state.Release.Namespace `--redpanda-cluster-name` (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (printf "--selector=helm.sh/chart=%s,app.kubernetes.io/name=%s,app.kubernetes.io/instance=%s" (get (fromJson (include "redpanda.ChartLabel" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") $state.Dot.Release.Name) `--run-broker-probe` `--broker-probe-broker-url` (get (fromJson (include "redpanda.adminURLsCLI" (dict "a" (list $state)))) "r")) -}}
{{- if $pool.Statefulset.sideCars.brokerDecommissioner.enabled -}}
{{- $args = (concat (default (list) $args) (default (list) (list `--run-decommissioner` (printf "--decommission-vote-interval=%s" $pool.Statefulset.sideCars.brokerDecommissioner.decommissionAfter) (printf "--decommission-requeue-timeout=%s" $pool.Statefulset.sideCars.brokerDecommissioner.decommissionRequeueTimeout) `--decommission-vote-count=2`))) -}}
{{- end -}}
{{- $sasl_8 := $state.Values.auth.sasl -}}
{{- if (and (and $sasl_8.enabled (ne $sasl_8.secretRef "")) $pool.Statefulset.sideCars.configWatcher.enabled) -}}
{{- $args = (concat (default (list) $args) (default (list) (list `--watch-users` `--users-directory=/etc/secrets/users/`))) -}}
{{- end -}}
{{- if $pool.Statefulset.sideCars.pvcUnbinder.enabled -}}
{{- $args = (concat (default (list) $args) (default (list) (list `--run-pvc-unbinder` (printf "--pvc-unbinder-timeout=%s" $pool.Statefulset.sideCars.pvcUnbinder.unbindAfter)))) -}}
{{- end -}}
{{- $args = (concat (default (list) $args) (default (list) $pool.Statefulset.sideCars.args)) -}}
{{- $volumeMounts := (concat (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "config" "mountPath" "/etc/redpanda")) (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "kube-api-access" "mountPath" "/var/run/secrets/kubernetes.io/serviceaccount" "readOnly" true)))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "sidecar" "image" (printf `%s:%s` $pool.Statefulset.sideCars.image.repository $pool.Statefulset.sideCars.image.tag) "command" (list `/redpanda-operator`) "args" (concat (default (list) (list `supervisor` `--`)) (default (list) $args)) "env" (concat (default (list) (get (fromJson (include "redpanda.rpkEnvVars" (dict "a" (list $state (coalesce nil))))) "r")) (default (list) (get (fromJson (include "redpanda.statefulSetRedpandaEnv" (dict "a" (list)))) "r"))) "volumeMounts" $volumeMounts "readinessProbe" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "httpGet" (mustMergeOverwrite (dict "port" 0) (dict "path" "/healthz" "port" (8093 | int))))) (dict "failureThreshold" (3 | int) "initialDelaySeconds" (1 | int) "periodSeconds" (10 | int) "successThreshold" (1 | int) "timeoutSeconds" (0 | int)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkEnvVars" -}}
{{- $state := (index .a 0) -}}
{{- $envVars := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne (toJson $state.Values.auth.sasl) "null") $state.Values.auth.sasl.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $envVars) (default (list) (get (fromJson (include "redpanda.BootstrapUser.RpkEnvironment" (dict "a" (list $state.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r"))))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $envVars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.bootstrapEnvVars" -}}
{{- $state := (index .a 0) -}}
{{- $envVars := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne (toJson $state.Values.auth.sasl) "null") $state.Values.auth.sasl.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $envVars) (default (list) (get (fromJson (include "redpanda.BootstrapUser.BootstrapEnvironment" (dict "a" (list $state.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r"))))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $envVars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSets" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $sets := (list (get (fromJson (include "redpanda.StatefulSet" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" ""))) (dict "Statefulset" $state.Values.statefulset)))))) "r")) -}}
{{- range $_, $set := $state.Pools -}}
{{- $sets = (concat (default (list) $sets) (list (get (fromJson (include "redpanda.StatefulSet" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $sets) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StatefulSet" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $poolLabels := (dict) -}}
{{- if (ne $pool.Name "") -}}
{{- $_ := (set $poolLabels "cluster.redpanda.com/nodepool-name" $pool.Name) -}}
{{- $_ := (set $poolLabels "cluster.redpanda.com/nodepool-generation" $pool.Generation) -}}
{{- end -}}
{{- $set := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "serviceName" "" "updateStrategy" (dict)) "status" (dict "replicas" 0 "availableReplicas" 0)) (mustMergeOverwrite (dict) (dict "apiVersion" "apps/v1" "kind" "StatefulSet")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")) "namespace" $state.Release.Namespace "labels" (merge (dict) (dict "app.kubernetes.io/component" (printf "%s%s" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r"))) $poolLabels (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r")))) "spec" (mustMergeOverwrite (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "serviceName" "" "updateStrategy" (dict)) (dict "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "redpanda.StatefulSetPodLabelsSelector" (dict "a" (list $state $pool)))) "r"))) "serviceName" (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") "replicas" ($pool.Statefulset.replicas | int) "updateStrategy" $pool.Statefulset.updateStrategy "podManagementPolicy" "Parallel" "template" (get (fromJson (include "redpanda.StrategicMergePatch" (dict "a" (list (get (fromJson (include "redpanda.StructuredTpl" (dict "a" (list $state $pool.Statefulset.podTemplate)))) "r") (get (fromJson (include "redpanda.StrategicMergePatch" (dict "a" (list (get (fromJson (include "redpanda.StructuredTpl" (dict "a" (list $state $state.Values.podTemplate)))) "r") (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" (get (fromJson (include "redpanda.StatefulSetPodLabels" (dict "a" (list $state $pool)))) "r") "annotations" (dict "config.redpanda.com/checksum" (get (fromJson (include "redpanda.statefulSetChecksumAnnotation" (dict "a" (list $state $pool)))) "r")))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "automountServiceAccountToken" false "serviceAccountName" (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r") "initContainers" (get (fromJson (include "redpanda.StatefulSetInitContainers" (dict "a" (list $state $pool)))) "r") "containers" (get (fromJson (include "redpanda.StatefulSetContainers" (dict "a" (list $state $pool)))) "r") "volumes" (get (fromJson (include "redpanda.StatefulSetVolumes" (dict "a" (list $state $pool)))) "r"))))))))) "r"))))) "r") "volumeClaimTemplates" (coalesce nil))))) -}}
{{- if (or $state.Values.storage.persistentVolume.enabled ((and (get (fromJson (include "redpanda.Storage.IsTieredStorageEnabled" (dict "a" (list $state.Values.storage)))) "r") (eq (get (fromJson (include "redpanda.Storage.TieredMountType" (dict "a" (list $state.Values.storage)))) "r") "persistentVolume")))) -}}
{{- $t_9 := (get (fromJson (include "redpanda.volumeClaimTemplateDatadir" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $t_9) "null") -}}
{{- $_ := (set $set.spec "volumeClaimTemplates" (concat (default (list) $set.spec.volumeClaimTemplates) (list $t_9))) -}}
{{- end -}}
{{- $t_10 := (get (fromJson (include "redpanda.volumeClaimTemplateTieredStorageDir" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $t_10) "null") -}}
{{- $_ := (set $set.spec "volumeClaimTemplates" (concat (default (list) $set.spec.volumeClaimTemplates) (list $t_10))) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $set) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.semver" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (trimPrefix "v" (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.statefulSetChecksumAnnotation" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $dependencies := (coalesce nil) -}}
{{- $dependencies = (concat (default (list) $dependencies) (list (get (fromJson (include "redpanda.RedpandaConfigFile" (dict "a" (list $state false $pool)))) "r"))) -}}
{{- if $state.Values.external.enabled -}}
{{- $dependencies = (concat (default (list) $dependencies) (list (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r"))) -}}
{{- if (empty $state.Values.external.addresses) -}}
{{- $dependencies = (concat (default (list) $dependencies) (list "")) -}}
{{- else -}}
{{- $dependencies = (concat (default (list) $dependencies) (list $state.Values.external.addresses)) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (sha256sum (toJson $dependencies))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.volumeClaimTemplateDatadir" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.storage.persistentVolume.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $pvc := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "resources" (dict)) "status" (dict)) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" "datadir" "labels" (merge (dict) (dict `app.kubernetes.io/name` (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") `app.kubernetes.io/instance` $state.Release.Name `app.kubernetes.io/component` (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r")) $state.Values.storage.persistentVolume.labels $state.Values.commonLabels) "annotations" (default (coalesce nil) $state.Values.storage.persistentVolume.annotations))) "spec" (mustMergeOverwrite (dict "resources" (dict)) (dict "accessModes" (list "ReadWriteOnce") "resources" (mustMergeOverwrite (dict) (dict "requests" (dict "storage" $state.Values.storage.persistentVolume.size))))))) -}}
{{- if (not (empty $state.Values.storage.persistentVolume.storageClass)) -}}
{{- if (eq $state.Values.storage.persistentVolume.storageClass "-") -}}
{{- $_ := (set $pvc.spec "storageClassName" "") -}}
{{- else -}}
{{- $_ := (set $pvc.spec "storageClassName" $state.Values.storage.persistentVolume.storageClass) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $pvc) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.volumeClaimTemplateTieredStorageDir" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not (get (fromJson (include "redpanda.Storage.IsTieredStorageEnabled" (dict "a" (list $state.Values.storage)))) "r")) (ne (get (fromJson (include "redpanda.Storage.TieredMountType" (dict "a" (list $state.Values.storage)))) "r") "persistentVolume")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $pvc := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "resources" (dict)) "status" (dict)) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (default "tiered-storage-dir" $state.Values.storage.persistentVolume.nameOverwrite) "labels" (merge (dict) (dict `app.kubernetes.io/name` (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") `app.kubernetes.io/instance` $state.Release.Name `app.kubernetes.io/component` (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r")) (get (fromJson (include "redpanda.Storage.TieredPersistentVolumeLabels" (dict "a" (list $state.Values.storage)))) "r") $state.Values.commonLabels) "annotations" (default (coalesce nil) (get (fromJson (include "redpanda.Storage.TieredPersistentVolumeAnnotations" (dict "a" (list $state.Values.storage)))) "r")))) "spec" (mustMergeOverwrite (dict "resources" (dict)) (dict "accessModes" (list "ReadWriteOnce") "resources" (mustMergeOverwrite (dict) (dict "requests" (dict "storage" (index (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r") `cloud_storage_cache_size`)))))))) -}}
{{- $sc_11 := (get (fromJson (include "redpanda.Storage.TieredPersistentVolumeStorageClass" (dict "a" (list $state.Values.storage)))) "r") -}}
{{- if (eq $sc_11 "-") -}}
{{- $_ := (set $pvc.spec "storageClassName" "") -}}
{{- else -}}{{- if (not (empty $sc_11)) -}}
{{- $_ := (set $pvc.spec "storageClassName" $sc_11) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $pvc) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StorageTieredConfig" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

