{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/multicluster/v25/chart.go" */ -}}

{{- define "multicluster.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_ := (get (fromJson (include "multicluster.Validate" (dict "a" (list $dot)))) "r") -}}
{{- $manifests := (list (get (fromJson (include "multicluster.Service" (dict "a" (list $dot)))) "r") (get (fromJson (include "multicluster.ServiceAccount" (dict "a" (list $dot)))) "r") (get (fromJson (include "multicluster.Deployment" (dict "a" (list $dot)))) "r") (get (fromJson (include "multicluster.PreInstallCRDJob" (dict "a" (list $dot)))) "r") (get (fromJson (include "multicluster.CRDJobServiceAccount" (dict "a" (list $dot)))) "r")) -}}
{{- range $_, $cr := (get (fromJson (include "multicluster.ClusterRoles" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $cr)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $crb := (get (fromJson (include "multicluster.ClusterRoleBindings" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $crb)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

