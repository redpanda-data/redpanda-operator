{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/telemetry.go" */ -}}

{{- define "_redpanda.MetricsFromDir" -}}
{{- $tag := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_40_ok_err := (list (semverCompare ">=26.3.0-0" $tag) nil) -}}
{{- $ok := (index $_40_ok_err 0) -}}
{{- $err := (index $_40_ok_err 1) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (and (eq (toJson $err) "null") $ok)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.MetricsConfigMapName" -}}
{{- $poolFullname := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%.51s-k8s-metrics" $poolFullname)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.MetricsConfigMapData" -}}
{{- $values := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $data := (dict) -}}
{{- range $_, $value := $values -}}
{{- $_ := (set $data $value.name $value.value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $data) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.MetricsVolume" -}}
{{- $poolFullname := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "_redpanda.MetricsConfigMapName" (dict "a" (list $poolFullname)))) "r"))) (dict "optional" true)))) (dict "name" "k8s-metrics"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.MetricsVolumeMount" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "k8s-metrics" "mountPath" "/etc/redpanda-k8s-metrics" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.MetricsDirEnv" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_DIR" "value" "/etc/redpanda-k8s-metrics")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

