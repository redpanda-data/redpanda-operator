{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/apiservice.go" */ -}}

{{- define "operator.APIService" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.experimental.apiServer.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-virtual-server" (get (fromJson (include "operator.Name" (dict "a" (list $dot)))) "r")) "namespace" $dot.Release.Namespace "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "spec" (mustMergeOverwrite (dict) (dict "selector" (get (fromJson (include "operator.SelectorLabels" (dict "a" (list $dot)))) "r") "ports" (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "port" ((443 | int) | int) "targetPort" (9050 | int))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.APIServiceCertificate" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.experimental.apiServer.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-virtual-server-certificate" (get (fromJson (include "operator.Name" (dict "a" (list $dot)))) "r")) "namespace" $dot.Release.Namespace "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.APIServices" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "operator.APIServiceV1Alpha1" (dict "a" (list $dot)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.APIServiceV1Alpha1" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.experimental.apiServer.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "groupPriorityMinimum" 0 "versionPriority" 0) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "apiregistration.k8s.io/v1" "kind" "APIService")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" "v1alpha1.virtual.cluster.redpanda.com" "namespace" $dot.Release.Namespace "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "spec" (mustMergeOverwrite (dict "groupPriorityMinimum" 0 "versionPriority" 0) (dict "group" "virtual.cluster.redpanda.com" "groupPriorityMinimum" (1000 | int) "versionPriority" (15 | int) "service" (mustMergeOverwrite (dict) (dict "namespace" $dot.Release.Namespace "name" (printf "%s-virtual-server" (get (fromJson (include "operator.Name" (dict "a" (list $dot)))) "r")))) "version" "v1alpha1"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

