{{- /* Generated from "post_install_crd_job.go" */ -}}

{{- define "operator.PostInstallCRDJob" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (and (not $values.crds.install) (not $values.crds.experimental)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $job := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil)))) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "batch/v1" "kind" "Job")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s-crds" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r")) "namespace" $dot.Release.Namespace "labels" (merge (dict) (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r")) "annotations" (dict "helm.sh/hook" "post-install,post-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation" "helm.sh/hook-weight" "-5"))) "spec" (mustMergeOverwrite (dict "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil)))) (dict "template" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "annotations" $values.podAnnotations "labels" (merge (dict) (get (fromJson (include "operator.SelectorLabels" (dict "a" (list $dot)))) "r") $values.podLabels))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "restartPolicy" "OnFailure" "automountServiceAccountToken" false "terminationGracePeriodSeconds" ((10 | int64) | int64) "imagePullSecrets" $values.imagePullSecrets "serviceAccountName" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "nodeSelector" $values.nodeSelector "tolerations" $values.tolerations "volumes" (get (fromJson (include "operator.operatorPodVolumes" (dict "a" (list $dot)))) "r") "containers" (get (fromJson (include "operator.crdJobContainers" (dict "a" (list $dot)))) "r"))))))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $job) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.crdJobContainers" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $args := (list "crd") -}}
{{- if $values.crds.experimental -}}
{{- $args = (concat (default (list) $args) (list "--experimental")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "crd-installation" "image" (get (fromJson (include "operator.containerImage" (dict "a" (list $dot)))) "r") "imagePullPolicy" $values.image.pullPolicy "command" (list "/redpanda-operator") "args" $args "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false)) "volumeMounts" (get (fromJson (include "operator.operatorPodVolumesMounts" (dict "a" (list $dot)))) "r") "resources" $values.resources)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

