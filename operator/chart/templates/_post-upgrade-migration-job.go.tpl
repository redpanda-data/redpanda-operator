{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/post_upgrade_migration_job.go" */ -}}

{{- define "operator.PostUpgradeMigrationJob" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil)))) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "batch/v1" "kind" "Job")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-migration" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r")) "namespace" $dot.Release.Namespace "labels" (merge (dict) (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r")) "annotations" (dict "helm.sh/hook" "post-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation,hook-succeeded,hook-failed" "helm.sh/hook-weight" "-4"))) "spec" (mustMergeOverwrite (dict "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil)))) (dict "template" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "annotations" $values.podAnnotations "labels" (merge (dict) (get (fromJson (include "operator.SelectorLabels" (dict "a" (list $dot)))) "r") $values.podLabels))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "restartPolicy" "OnFailure" "automountServiceAccountToken" false "terminationGracePeriodSeconds" ((10 | int64) | int64) "imagePullSecrets" $values.imagePullSecrets "serviceAccountName" (get (fromJson (include "operator.MigrationJobServiceAccountName" (dict "a" (list $dot)))) "r") "nodeSelector" $values.nodeSelector "tolerations" $values.tolerations "volumes" (list (get (fromJson (include "operator.serviceAccountTokenVolume" (dict "a" (list)))) "r")) "containers" (get (fromJson (include "operator.migrationJobContainers" (dict "a" (list $dot)))) "r")))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.migrationJobContainers" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $args := (list "migration") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "migration" "image" (get (fromJson (include "operator.containerImage" (dict "a" (list $dot)))) "r") "imagePullPolicy" $values.image.pullPolicy "command" (list "/redpanda-operator") "args" $args "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false)) "volumeMounts" (list (get (fromJson (include "operator.serviceAccountTokenVolumeMount" (dict "a" (list)))) "r")) "resources" $values.resources)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

