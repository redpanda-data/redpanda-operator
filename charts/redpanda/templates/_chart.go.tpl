{{- /* Generated from "chart.go" */ -}}

{{- define "redpanda.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $manifests := (get (fromJson (include "redpanda.renderResources" (dict "a" (list $dot (coalesce nil))))) "r") -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.StatefulSets" (dict "a" (list $dot (coalesce nil))))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.renderResources" -}}
{{- $dot := (index .a 0) -}}
{{- $pools := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_ := (get (fromJson (include "redpanda.checkVersion" (dict "a" (list $dot)))) "r") -}}
{{- $manifests := (list (get (fromJson (include "redpanda.NodePortService" (dict "a" (list $dot)))) "r") (get (fromJson (include "redpanda.PodDisruptionBudget" (dict "a" (list $dot $pools)))) "r") (get (fromJson (include "redpanda.ServiceAccount" (dict "a" (list $dot)))) "r") (get (fromJson (include "redpanda.ServiceInternal" (dict "a" (list $dot)))) "r") (get (fromJson (include "redpanda.ServiceMonitor" (dict "a" (list $dot)))) "r") (get (fromJson (include "redpanda.PostInstallUpgradeJob" (dict "a" (list $dot)))) "r")) -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ConfigMaps" (dict "a" (list $dot $pools)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.CertIssuers" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RootCAs" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClientCerts" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Roles" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoles" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RoleBindings" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoleBindings" (dict "a" (list $dot)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.LoadBalancerServices" (dict "a" (list $dot $pools)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Secrets" (dict "a" (list $dot $pools)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $manifests = (concat (default (list) $manifests) (default (list) (get (fromJson (include "redpanda.consoleChartIntegration" (dict "a" (list $dot $pools)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

