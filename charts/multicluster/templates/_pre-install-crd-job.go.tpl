{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/multicluster/v25/pre_install_crd_job.go" */ -}}

{{- define "multicluster.PreInstallCRDJob" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.installCRDs) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil)))) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "batch/v1" "kind" "Job")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s-crds" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r")) "namespace" $dot.Release.Namespace "labels" (merge (dict) (get (fromJson (include "multicluster.Labels" (dict "a" (list $dot)))) "r")) "annotations" (dict "helm.sh/hook" "pre-install,pre-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation,hook-succeeded,hook-failed" "helm.sh/hook-weight" "-5"))) "spec" (mustMergeOverwrite (dict "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil)))) (dict "template" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" (get (fromJson (include "multicluster.SelectorLabels" (dict "a" (list $dot)))) "r"))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "restartPolicy" "OnFailure" "terminationGracePeriodSeconds" ((10 | int64) | int64) "serviceAccountName" (get (fromJson (include "multicluster.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "containers" (get (fromJson (include "multicluster.crdJobContainers" (dict "a" (list $dot)))) "r")))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.crdJobContainers" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $args := (list "crd" "--multicluster") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "crd-installation" "image" (get (fromJson (include "multicluster.containerImage" (dict "a" (list $dot)))) "r") "command" (list "/redpanda-operator") "args" $args "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false)) "resources" $values.resources)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

