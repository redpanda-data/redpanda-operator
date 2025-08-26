{{- /* Generated from "chart.go" */ -}}

{{- define "redpanda.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $state := (get (fromJson (include "redpanda.renderStateFromDot" (dict "a" (list $dot)))) "r") -}}
{{- $manifests := (get (fromJson (include "redpanda.renderResources" (dict "a" (list $state)))) "r") -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.StatefulSets" (dict "a" (list $state)))) "r") -}}
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
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_ := (get (fromJson (include "redpanda.checkVersion" (dict "a" (list $state)))) "r") -}}
{{- $manifests := (list (get (fromJson (include "redpanda.NodePortService" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.PodDisruptionBudget" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceAccount" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceInternal" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceMonitor" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.PostInstallUpgradeJob" (dict "a" (list $state)))) "r")) -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ConfigMaps" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.CertIssuers" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RootCAs" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClientCerts" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Roles" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoles" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RoleBindings" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoleBindings" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.LoadBalancerServices" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Secrets" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $manifests = (concat (default (list) $manifests) (default (list) (get (fromJson (include "redpanda.consoleChartIntegration" (dict "a" (list $state)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.checkVersion" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (not (get (fromJson (include "redpanda.RedpandaAtLeast_22_2_0" (dict "a" (list $state)))) "r")) (not $state.Values.force)) -}}
{{- $sv := (get (fromJson (include "redpanda.semver" (dict "a" (list $state)))) "r") -}}
{{- $_ := (fail (printf "Error: The Redpanda version (%s) is no longer supported \nTo accept this risk, run the upgrade again adding `--force=true`\n" $sv)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

