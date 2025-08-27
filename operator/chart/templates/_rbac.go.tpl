{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/rbac.go" */ -}}

{{- define "operator.rbacBundles" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "Enabled" false "Name" "" "Subject" "" "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "Enabled" true "Subject" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "RuleFiles" (dict "files/rbac/leader-election.ClusterRole.yaml" true "files/rbac/leader-election.Role.yaml" true "files/rbac/pvcunbinder.ClusterRole.yaml" true "files/rbac/pvcunbinder.Role.yaml" true "files/rbac/rack-awareness.ClusterRole.yaml" true "files/rbac/rpk-debug-bundle.Role.yaml" true "files/rbac/sidecar.Role.yaml" true "files/rbac/v1-manager.ClusterRole.yaml" $values.vectorizedControllers.enabled "files/rbac/v1-manager.Role.yaml" $values.vectorizedControllers.enabled "files/rbac/v2-manager.ClusterRole.yaml" true))) (mustMergeOverwrite (dict "Enabled" false "Name" "" "Subject" "" "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "additional-controllers")))) "r") "Enabled" $values.rbac.createAdditionalControllerCRs "Subject" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "RuleFiles" (dict "files/rbac/decommission.ClusterRole.yaml" true "files/rbac/decommission.Role.yaml" true "files/rbac/node-watcher.ClusterRole.yaml" true "files/rbac/node-watcher.Role.yaml" true "files/rbac/old-decommission.ClusterRole.yaml" true "files/rbac/old-decommission.Role.yaml" true "files/rbac/pvcunbinder.ClusterRole.yaml" true "files/rbac/pvcunbinder.Role.yaml" true))) (mustMergeOverwrite (dict "Enabled" false "Name" "" "Subject" "" "RuleFiles" (coalesce nil) "Annotations" (coalesce nil)) (dict "Name" (get (fromJson (include "operator.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "Enabled" (or $values.crds.enabled $values.crds.experimental) "Subject" (get (fromJson (include "operator.CRDJobServiceAccountName" (dict "a" (list $dot)))) "r") "Annotations" (dict "helm.sh/hook" "pre-install,pre-upgrade" "helm.sh/hook-delete-policy" "before-hook-creation,hook-succeeded,hook-failed" "helm.sh/hook-weight" "-10") "RuleFiles" (dict "files/rbac/crd-installation.ClusterRole.yaml" true))))) | toJson -}}
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
{{- $clusterRoles := (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRole")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "operator.cleanForK8sWithSuffix" (dict "a" (list (printf "%s%s" (printf "%s%s" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "-") $dot.Release.Namespace) "metrics-reader")))) "r") "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "annotations" $values.annotations)) "rules" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "verbs" (list "get") "nonResourceURLs" (list "/metrics"))))))) -}}
{{- range $_, $bundle := (get (fromJson (include "operator.rbacBundles" (dict "a" (list $dot)))) "r") -}}
{{- if (not $bundle.Enabled) -}}
{{- continue -}}
{{- end -}}
{{- $rules := (coalesce nil) -}}
{{- range $file, $enabled := $bundle.RuleFiles -}}
{{- if (not $enabled) -}}
{{- continue -}}
{{- end -}}
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
{{- range $_, $bundle := (get (fromJson (include "operator.rbacBundles" (dict "a" (list $dot)))) "r") -}}
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

