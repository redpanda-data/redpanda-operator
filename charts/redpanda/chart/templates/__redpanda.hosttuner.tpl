{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/hosttuner.go" */ -}}

{{- define "_redpanda.HostTunerDefaults" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "tune_disk_irq" true "tune_disk_scheduler" true "tune_disk_nomerges" true "tune_network" true "tune_fstrim" true "tune_disk_write_cache" true "tune_cpu" true)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.HostTunerVolumes" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $vols := (list) -}}
{{- range $_, $dir := (get (fromJson (include "_redpanda.hostTunerDirs" (dict "a" (list)))) "r") -}}
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

{{- define "_redpanda.HostTunerStateVolumeMount" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "host-tuner-state" "mountPath" "/var/run/redpanda_node_tuner_state.yaml" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.hostTunerDirs" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "bin" "sbin" "sys" "proc" "etc" "usr" "lib" "lib64" "dev" "var" "run")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.hostTunerVolumeMounts" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $readOnlyDirs := (dict "bin" true "sbin" true "usr" true "lib" true "lib64" true) -}}
{{- $mounts := (list) -}}
{{- range $_, $dir := (get (fromJson (include "_redpanda.hostTunerDirs" (dict "a" (list)))) "r") -}}
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

