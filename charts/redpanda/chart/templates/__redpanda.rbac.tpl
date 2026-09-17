{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/rbac.go" */ -}}

{{- define "_redpanda.RoleSet.roleToggles" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "decommission" $r.Roles.Decommission "pvcunbinder" $r.Roles.PVCUnbinder "rpk-debug-bundle" $r.Roles.RPKDebugBundle "sidecar" $r.Roles.Sidecar)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.clusterRoleToggles" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "decommission" $r.ClusterRoles.Decommission "metrics-reader" $r.ClusterRoles.MetricsReader "pvcunbinder" $r.ClusterRoles.PVCUnbinder "rack-awareness" $r.ClusterRoles.RackAwareness "stretch-rack-awareness" $r.ClusterRoles.StretchRackAwareness)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.RoleName" -}}
{{- $r := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s-%s" $r.Prefix $name)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.ClusterRoleName" -}}
{{- $r := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.cleanForK8s" (dict "a" (list (printf "%s-%s-%s" $r.Prefix $r.Namespace $name))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.Render" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $objs := (coalesce nil) -}}
{{- range $_, $obj := (get (fromJson (include "_redpanda.RoleSet.renderRoles" (dict "a" (list $r)))) "r") -}}
{{- $objs = (concat (default (list) $objs) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "_redpanda.RoleSet.renderClusterRoles" (dict "a" (list $r)))) "r") -}}
{{- $objs = (concat (default (list) $objs) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "_redpanda.RoleSet.renderRoleBindings" (dict "a" (list $r)))) "r") -}}
{{- $objs = (concat (default (list) $objs) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "_redpanda.RoleSet.renderClusterRoleBindings" (dict "a" (list $r)))) "r") -}}
{{- $objs = (concat (default (list) $objs) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $objs) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.renderRoles" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $catalog := (get (fromJson (include "_redpanda.roleRules" (dict "a" (list)))) "r") -}}
{{- $toggles := (get (fromJson (include "_redpanda.RoleSet.roleToggles" (dict "a" (list $r)))) "r") -}}
{{- $roles := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $toggles))))) "r") -}}
{{- if (not (ternary (index $toggles $name) false (hasKey $toggles $name))) -}}
{{- continue -}}
{{- end -}}
{{- $roles = (concat (default (list) $roles) (list (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "Role")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "_redpanda.RoleSet.RoleName" (dict "a" (list $r $name)))) "r") "namespace" $r.Namespace "labels" $r.Labels "annotations" $r.Annotations)) "rules" (index $catalog $name))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $roles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.renderClusterRoles" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $catalog := (get (fromJson (include "_redpanda.clusterRoleRules" (dict "a" (list)))) "r") -}}
{{- $toggles := (get (fromJson (include "_redpanda.RoleSet.clusterRoleToggles" (dict "a" (list $r)))) "r") -}}
{{- $clusterRoles := (coalesce nil) -}}
{{- range $_, $name := (get (fromJson (include "_shims.slices_Sorted" (dict "a" (list (keys $toggles))))) "r") -}}
{{- if (not (ternary (index $toggles $name) false (hasKey $toggles $name))) -}}
{{- continue -}}
{{- end -}}
{{- $clusterRoles = (concat (default (list) $clusterRoles) (list (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRole")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "_redpanda.RoleSet.ClusterRoleName" (dict "a" (list $r $name)))) "r") "labels" $r.Labels "annotations" $r.Annotations)) "rules" (index $catalog $name))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $clusterRoles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.renderRoleBindings" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $roleBindings := (coalesce nil) -}}
{{- range $_, $role := (get (fromJson (include "_redpanda.RoleSet.renderRoles" (dict "a" (list $r)))) "r") -}}
{{- $roleBindings = (concat (default (list) $roleBindings) (list (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "RoleBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $role.metadata.name "namespace" $r.Namespace "labels" $r.Labels "annotations" $r.Annotations)) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "Role" "name" $role.metadata.name)) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" $r.ServiceAccount "namespace" $r.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $roleBindings) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.RoleSet.renderClusterRoleBindings" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $crbs := (coalesce nil) -}}
{{- range $_, $clusterRole := (get (fromJson (include "_redpanda.RoleSet.renderClusterRoles" (dict "a" (list $r)))) "r") -}}
{{- $crbs = (concat (default (list) $crbs) (list (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRoleBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $clusterRole.metadata.name "labels" $r.Labels "annotations" $r.Annotations)) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "ClusterRole" "name" $clusterRole.metadata.name)) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" $r.ServiceAccount "namespace" $r.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $crbs) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ServiceAccount" -}}
{{- $name := (index .a 0) -}}
{{- $namespace := (index .a 1) -}}
{{- $labels := (index .a 2) -}}
{{- $annotations := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "ServiceAccount")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $name "namespace" $namespace "labels" $labels "annotations" $annotations)) "automountServiceAccountToken" false))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.roleRules" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "decommission" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "events") "verbs" (list "create" "patch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "persistentvolumeclaims") "verbs" (list "delete" "get" "list" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "pods" "secrets") "verbs" (list "get" "list" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "apps") "resources" (list "statefulsets") "verbs" (list "get" "list" "watch")))) "pvcunbinder" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "persistentvolumeclaims" "pods") "verbs" (list "delete" "get" "list" "watch")))) "rpk-debug-bundle" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "configmaps" "endpoints" "events" "limitranges" "persistentvolumeclaims" "pods" "pods/log" "replicationcontrollers" "resourcequotas" "serviceaccounts" "services") "verbs" (list "get" "list")))) "sidecar" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "coordination.k8s.io") "resources" (list "leases") "verbs" (list "create" "delete" "get" "list" "patch" "update" "watch")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.clusterRoleRules" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "decommission" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "persistentvolumes") "verbs" (list "patch")))) "metrics-reader" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "nonResourceURLs" (list "/metrics") "verbs" (list "get")))) "pvcunbinder" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "nodes") "verbs" (list "get" "list"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "persistentvolumes") "verbs" (list "get" "list" "patch" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "pods") "verbs" (list "list" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "cluster.redpanda.com") "resources" (list "redpandas" "stretchclusters") "verbs" (list "get" "list" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "events.k8s.io") "resources" (list "events") "verbs" (list "create" "patch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "redpanda.vectorized.io") "resources" (list "clusters") "verbs" (list "get" "list" "watch"))) (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "storage.k8s.io") "resources" (list "storageclasses") "verbs" (list "get")))) "rack-awareness" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "nodes") "verbs" (list "get")))) "stretch-rack-awareness" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "") "resources" (list "nodes") "verbs" (list "get" "list" "watch")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.cleanForK8s" -}}
{{- $in := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $in)))) "r") | int) (63 | int)) -}}
{{- $in = (substr 0 (63 | int) $in) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (trimSuffix "-" $in)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

