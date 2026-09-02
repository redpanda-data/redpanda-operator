{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/statefulset.go" */ -}}

{{- define "_redpanda.KubeTokenAPIVolume" -}}
{{- $name := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "projected" (mustMergeOverwrite (dict "sources" (coalesce nil)) (dict "defaultMode" (420 | int) "sources" (list (mustMergeOverwrite (dict) (dict "serviceAccountToken" (mustMergeOverwrite (dict "path" "") (dict "path" "token" "expirationSeconds" ((3607 | int) | int64))))) (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" "kube-root-ca.crt")) (dict "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "ca.crt" "path" "ca.crt"))))))) (mustMergeOverwrite (dict) (dict "downwardAPI" (mustMergeOverwrite (dict) (dict "items" (list (mustMergeOverwrite (dict "path" "") (dict "path" "namespace" "fieldRef" (mustMergeOverwrite (dict "fieldPath" "") (dict "apiVersion" "v1" "fieldPath" "metadata.namespace")))))))))))))) (dict "name" $name))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

