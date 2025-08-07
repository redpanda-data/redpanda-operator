{{- /* Generated from "serviceaccount.go" */ -}}

{{- define "operator.ServiceAccountName" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $values.serviceAccount.name (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.CRDJobServiceAccountName" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s%s" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "-crd-job")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.ServiceAccount" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.serviceAccount.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "ServiceAccount" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace "annotations" $values.serviceAccount.annotations)) "automountServiceAccountToken" $values.serviceAccount.automountServiceAccountToken))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.CRDJobServiceAccount" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not ((or $values.crds.enabled (not $values.crds.experimental)))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "ServiceAccount" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "operator.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace "annotations" (merge (dict) (default (dict) $values.serviceAccount.annotations) (dict "helm.sh/hook" "pre-install,pre-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation,hook-succeeded,hook-failed" "helm.sh/hook-weight" "-10")))) "automountServiceAccountToken" false))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

