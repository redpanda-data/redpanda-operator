{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/rbac.go" */ -}}

{{- define "redpanda.RoleSet" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $annotations := (merge (dict) (dict) $state.Values.serviceAccount.annotations $state.Values.rbac.annotations (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Prefix" "" "Namespace" "" "Labels" (coalesce nil) "Annotations" (coalesce nil) "ServiceAccount" "" "Roles" (dict "Decommission" false "PVCUnbinder" false "RPKDebugBundle" false "Sidecar" false) "ClusterRoles" (dict "Decommission" false "MetricsReader" false "PVCUnbinder" false "RackAwareness" false "StretchRackAwareness" false)) (dict "Prefix" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") "Namespace" $state.Release.Namespace "Labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "Annotations" $annotations "ServiceAccount" (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r") "Roles" (mustMergeOverwrite (dict "Decommission" false "PVCUnbinder" false "RPKDebugBundle" false "Sidecar" false) (dict "Decommission" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.brokerDecommissioner.enabled) "PVCUnbinder" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.pvcUnbinder.enabled) "RPKDebugBundle" (and $state.Values.rbac.enabled $state.Values.rbac.rpkDebugBundle) "Sidecar" $state.Values.rbac.enabled)) "ClusterRoles" (mustMergeOverwrite (dict "Decommission" false "MetricsReader" false "PVCUnbinder" false "RackAwareness" false "StretchRackAwareness" false) (dict "Decommission" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.brokerDecommissioner.enabled) "PVCUnbinder" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.pvcUnbinder.enabled) "RackAwareness" (and $state.Values.rbac.enabled $state.Values.rackAwareness.enabled)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

