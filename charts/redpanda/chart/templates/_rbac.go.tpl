{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/rbac.go" */ -}}

{{- define "redpanda.Roles" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mapping := (dict "files/sidecar.Role.yaml" $state.Values.rbac.enabled "files/pvcunbinder.Role.yaml" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.pvcUnbinder.enabled) "files/decommission.Role.yaml" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.brokerDecommissioner.enabled) "files/rpk-debug-bundle.Role.yaml" (and $state.Values.rbac.enabled $state.Values.rbac.rpkDebugBundle)) -}}
{{- $roles := (coalesce nil) -}}
{{- range $file, $enabled := $mapping -}}
{{- if (not $enabled) -}}
{{- continue -}}
{{- end -}}
{{- $role := (get (fromJson (include "_shims.fromYaml" (dict "a" (list ($state.Dot.Files.Get $file))))) "r") -}}
{{- $_ := (set $role.metadata "name" (printf "%s-%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $role.metadata.name)) -}}
{{- $_ := (set $role.metadata "namespace" $state.Release.Namespace) -}}
{{- $_ := (set $role.metadata "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $role.metadata "annotations" (merge (dict) (dict) $state.Values.serviceAccount.annotations $state.Values.rbac.annotations)) -}}
{{- $roles = (concat (default (list) $roles) (list $role)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $roles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ClusterRoles" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mapping := (dict "files/pvcunbinder.ClusterRole.yaml" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.pvcUnbinder.enabled) "files/decommission.ClusterRole.yaml" (and $state.Values.rbac.enabled $state.Values.statefulset.sideCars.brokerDecommissioner.enabled) "files/rack-awareness.ClusterRole.yaml" (and $state.Values.rbac.enabled $state.Values.rackAwareness.enabled)) -}}
{{- $clusterRoles := (coalesce nil) -}}
{{- range $file, $enabled := $mapping -}}
{{- if (not $enabled) -}}
{{- continue -}}
{{- end -}}
{{- $role := (get (fromJson (include "_shims.fromYaml" (dict "a" (list ($state.Dot.Files.Get $file))))) "r") -}}
{{- $_ := (set $role.metadata "name" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list (printf "%s-%s-%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $state.Release.Namespace $role.metadata.name))))) "r")) -}}
{{- $_ := (set $role.metadata "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $role.metadata "annotations" (merge (dict) (dict) $state.Values.serviceAccount.annotations $state.Values.rbac.annotations)) -}}
{{- $clusterRoles = (concat (default (list) $clusterRoles) (list $role)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $clusterRoles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RoleBindings" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $roleBindings := (coalesce nil) -}}
{{- range $_, $role := (get (fromJson (include "redpanda.Roles" (dict "a" (list $state)))) "r") -}}
{{- $roleBindings = (concat (default (list) $roleBindings) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "RoleBinding")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" $role.metadata.name "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace "annotations" (merge (dict) (dict) $state.Values.serviceAccount.annotations $state.Values.rbac.annotations))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "Role" "name" $role.metadata.name)) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $roleBindings) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ClusterRoleBindings" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $crbs := (coalesce nil) -}}
{{- range $_, $clusterRole := (get (fromJson (include "redpanda.ClusterRoles" (dict "a" (list $state)))) "r") -}}
{{- $crbs = (concat (default (list) $crbs) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRoleBinding")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" $clusterRole.metadata.name "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (merge (dict) (dict) $state.Values.serviceAccount.annotations $state.Values.rbac.annotations))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "ClusterRole" "name" $clusterRole.metadata.name)) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "redpanda.ServiceAccountName" (dict "a" (list $state)))) "r") "namespace" $state.Release.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $crbs) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

