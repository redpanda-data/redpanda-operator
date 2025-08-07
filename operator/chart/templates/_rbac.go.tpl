{{- /* Generated from "rbac.go" */ -}}

{{- define "operator.clusterRoleBundles" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.rbac.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "Subject" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "Enabled" (eq $values.scope "Cluster") "RuleFiles" (list "files/rbac/leader-election.ClusterRole.yaml" "files/rbac/pvcunbinder.ClusterRole.yaml" "files/rbac/rack-awareness.ClusterRole.yaml" "files/rbac/v1-manager.ClusterRole.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "Subject" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "Enabled" (eq $values.scope "Namespace") "RuleFiles" (list "files/rbac/leader-election.ClusterRole.yaml" "files/rbac/v2-manager.ClusterRole.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "additional-controllers")))) "r") "Subject" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "Enabled" (and (eq $values.scope "Namespace") $values.rbac.createAdditionalControllerCRs) "RuleFiles" (list "files/rbac/decommission.ClusterRole.yaml" "files/rbac/node-watcher.ClusterRole.yaml" "files/rbac/old-decommission.ClusterRole.yaml" "files/rbac/pvcunbinder.ClusterRole.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "Enabled" (or $values.crds.enabled $values.crds.experimental) "Subject" (get (fromJson (include "operator.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "Annotations" (dict "helm.sh/hook" "pre-install,pre-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation,hook-succeeded,hook-failed" "helm.sh/hook-weight" "-10") "RuleFiles" (list "files/rbac/crd-installation.ClusterRole.yaml"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.ClusterRoles" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.rbac.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $clusterRoles := (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRole")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "metrics-reader")))) "r") "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "rules" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "verbs" (list "get") "nonResourceURLs" (list "/metrics"))))))) -}}
{{- range $_, $bundle := (get (fromJson (include "operator.clusterRoleBundles" (dict "a" (list $dot)))) "r") -}}
{{- if (not $bundle.Enabled) -}}
{{- continue -}}
{{- end -}}
{{- $rules := (coalesce nil) -}}
{{- range $_, $file := $bundle.RuleFiles -}}
{{- $clusterRole := (get (fromJson (include "_shims.fromYaml" (dict "a" (list ($dot.Files.Get $file))))) "r") -}}
{{- $rules = (concat (default (list) $rules) (default (list) $clusterRole.rules)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $clusterRoles = (concat (default (list) $clusterRoles) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRole")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s%s" (printf "%s%s" $bundle.Name "-") $dot.Release.Namespace) "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" (merge (dict) (default (dict) $values.annotations) (default (dict) $bundle.Annotations)))) "rules" $rules)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $clusterRoles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.Roles" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.rbac.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $bundles := (list (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "election-role")))) "r") "Enabled" true "RuleFiles" (list "files/rbac/leader-election.Role.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "Enabled" (eq $values.scope "Cluster") "RuleFiles" (list "files/rbac/pvcunbinder.Role.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "Enabled" (eq $values.scope "Namespace") "RuleFiles" (list "files/rbac/sidecar.Role.yaml" "files/rbac/v2-manager.Role.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (printf "%s%s" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "-additional-controllers") "Enabled" (and (eq $values.scope "Namespace") $values.rbac.createAdditionalControllerCRs) "RuleFiles" (list "files/rbac/decommission.Role.yaml" "files/rbac/node-watcher.Role.yaml" "files/rbac/old-decommission.Role.yaml" "files/rbac/pvcunbinder.Role.yaml"))) (mustMergeOverwrite (dict "Name" "" "Subject" "" "Enabled" false "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "rpk-bundle")))) "r") "Enabled" $values.rbac.createRPKBundleCRs "RuleFiles" (list "files/rbac/rpk-debug-bundle.Role.yaml")))) -}}
{{- $roles := (coalesce nil) -}}
{{- range $_, $bundle := $bundles -}}
{{- if (not $bundle.Enabled) -}}
{{- continue -}}
{{- end -}}
{{- $rules := (coalesce nil) -}}
{{- range $_, $file := $bundle.RuleFiles -}}
{{- $clusterRole := (get (fromJson (include "_shims.fromYaml" (dict "a" (list ($dot.Files.Get $file))))) "r") -}}
{{- $rules = (concat (default (list) $rules) (default (list) $clusterRole.rules)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $roles = (concat (default (list) $roles) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "Role")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" $bundle.Name "namespace" $dot.Release.Namespace "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "rules" $rules)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $roles) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.ClusterRoleBindings" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.rbac.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $bindings := (coalesce nil) -}}
{{- range $_, $bundle := (get (fromJson (include "operator.clusterRoleBundles" (dict "a" (list $dot)))) "r") -}}
{{- if (not $bundle.Enabled) -}}
{{- continue -}}
{{- end -}}
{{- $bindings = (concat (default (list) $bindings) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRoleBinding")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s%s" (printf "%s%s" $bundle.Name "-") $dot.Release.Namespace) "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" (merge (dict) (default (dict) $values.annotations) (default (dict) $bundle.Annotations)))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "ClusterRole" "name" (printf "%s%s" (printf "%s%s" $bundle.Name "-") $dot.Release.Namespace))) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" $bundle.Subject "namespace" $dot.Release.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $bindings) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.RoleBindings" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not $values.rbac.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $bindings := (coalesce nil) -}}
{{- range $_, $role := (get (fromJson (include "operator.Roles" (dict "a" (list $dot)))) "r") -}}
{{- $bindings = (concat (default (list) $bindings) (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "RoleBinding")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" $role.metadata.name "namespace" $dot.Release.Namespace "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "Role" "name" $role.metadata.name)) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace))))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $bindings) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

