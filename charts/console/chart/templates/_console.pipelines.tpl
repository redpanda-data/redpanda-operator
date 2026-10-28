{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/pipelines.go" */ -}}

{{- define "console.PipelinesNamespace" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (default $state.Namespace $state.Values.pipelines.namespace)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelinesViaServiceAccount" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (and $state.Values.pipelines.enabled (not $state.Values.pipelines.impersonateUser))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelinesConfig" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.Values.config) | toJson -}}
{{- break -}}
{{- end -}}
{{- $kubernetes := (merge (dict) (dict) (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "map[%s]%s" "string" "interface {}") (dig "pipelines" "kubernetes" (dict) $state.Values.config))))) "r")) -}}
{{- $_ := (set $kubernetes "enabled" true) -}}
{{- $_ := (set $kubernetes "namespace" (get (fromJson (include "console.PipelinesNamespace" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $kubernetes "clusterRef" $state.Values.pipelines.clusterRef) -}}
{{- $_ := (set $kubernetes "impersonateUser" $state.Values.pipelines.impersonateUser) -}}
{{- $pipelines := (merge (dict) (dict) (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "map[%s]%s" "string" "interface {}") (dig "pipelines" (dict) $state.Values.config))))) "r")) -}}
{{- $_ := (set $pipelines "kubernetes" $kubernetes) -}}
{{- $config := (merge (dict) (dict) $state.Values.config) -}}
{{- $_ := (set $config "pipelines" $pipelines) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $config) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelinesRole" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "console.pipelinesViaServiceAccount" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "Role")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-pipelines" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")) "namespace" (get (fromJson (include "console.PipelinesNamespace" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "rules" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "cluster.redpanda.com") "resources" (list "pipelines") "verbs" (list "get" "list" "watch" "create" "update" "patch" "delete"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelinesRoleBinding" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "console.pipelinesViaServiceAccount" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "RoleBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-pipelines" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")) "namespace" (get (fromJson (include "console.PipelinesNamespace" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "Role" "name" (printf "%s-pipelines" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")))) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineEditorClusterRole" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.pipelinesClusterRole" (dict "a" (list $state "pipeline-editor" (list "get" "list" "watch" "create" "update" "patch" "delete"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineViewerClusterRole" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.pipelinesClusterRole" (dict "a" (list $state "pipeline-viewer" (list "get" "list" "watch"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelinesClusterRole" -}}
{{- $state := (index .a 0) -}}
{{- $suffix := (index .a 1) -}}
{{- $verbs := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.pipelines.enabled) (not $state.Values.pipelines.impersonateUser)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRole")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.pipelinesClusterScopedName" (dict "a" (list $state $suffix)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "rules" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "cluster.redpanda.com") "resources" (list "pipelines") "verbs" $verbs)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelinesAdmissionPolicy" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not (get (fromJson (include "console.pipelinesViaServiceAccount" (dict "a" (list $state)))) "r")) (not $state.Values.pipelines.admissionPolicy.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $username := (printf "system:serviceaccount:%s:%s" $state.Namespace (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "admissionregistration.k8s.io/v1" "kind" "ValidatingAdmissionPolicy")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.pipelinesClusterScopedName" (dict "a" (list $state "pipelines")))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "spec" (mustMergeOverwrite (dict) (dict "failurePolicy" "Fail" "matchConstraints" (mustMergeOverwrite (dict) (dict "resourceRules" (list (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "apiGroups" (list "cluster.redpanda.com") "apiVersions" (list "*") "resources" (list "pipelines"))) (dict "operations" (list "CREATE" "UPDATE" "DELETE"))) (dict))))) "matchConditions" (list (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "console-service-account" "expression" (printf "request.userInfo.username == %q" $username)))) "variables" (list (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "spec" "expression" "request.operation == 'DELETE' || !has(object.spec) ? {} : dyn(object.spec)")) (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "oldSpec" "expression" "request.operation != 'UPDATE' || !has(oldObject.spec) ? {} : dyn(oldObject.spec)")) (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "allowedSpecFields" "expression" "['configYaml', 'displayName', 'description', 'tags', 'resources', 'paused', 'replicas', 'cluster']")) (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "changedSpecFields" "expression" "variables.spec.filter(f, !(f in variables.oldSpec) || variables.oldSpec[f] != variables.spec[f])")) (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "forbiddenSpecFields" "expression" "variables.changedSpecFields.filter(f, !(f in variables.allowedSpecFields))")) (mustMergeOverwrite (dict "name" "" "expression" "") (dict "name" "clusterRef" "expression" (printf "%q" $state.Values.pipelines.clusterRef)))) "validations" (list (mustMergeOverwrite (dict "expression" "") (dict "expression" "request.operation == 'DELETE' || (has(object.metadata.labels) && 'console.redpanda.com/managed-by' in object.metadata.labels && object.metadata.labels['console.redpanda.com/managed-by'] == 'console')" "message" "Redpanda Console may only write Pipelines labeled console.redpanda.com/managed-by=console")) (mustMergeOverwrite (dict "expression" "") (dict "expression" "request.operation == 'CREATE' || (has(oldObject.metadata.labels) && 'console.redpanda.com/managed-by' in oldObject.metadata.labels && oldObject.metadata.labels['console.redpanda.com/managed-by'] == 'console')" "message" "Redpanda Console may only update or delete Pipelines labeled console.redpanda.com/managed-by=console")) (mustMergeOverwrite (dict "expression" "") (dict "expression" "size(variables.forbiddenSpecFields) == 0" "messageExpression" "'Redpanda Console may only set spec fields ' + variables.allowedSpecFields.join(', ') + '; found ' + variables.forbiddenSpecFields.join(', ')")) (mustMergeOverwrite (dict "expression" "") (dict "expression" "!('resources' in variables.changedSpecFields) || variables.spec.resources.all(k, (k == 'requests' || k == 'limits') && dyn(variables.spec.resources[k]).all(r, r == 'cpu' || r == 'memory'))" "message" "Redpanda Console may only set cpu and memory in spec.resources.requests and spec.resources.limits")) (mustMergeOverwrite (dict "expression" "") (dict "expression" "!('cluster' in variables.changedSpecFields) || (variables.clusterRef != '' && variables.spec.cluster.all(f, f == 'clusterRef') && has(variables.spec.cluster.clusterRef) && dyn(variables.spec.cluster.clusterRef).all(f, f == 'name') && variables.spec.cluster.clusterRef.name == variables.clusterRef)" "messageExpression" "variables.clusterRef == '' ? 'Redpanda Console may not set spec.cluster' : 'Redpanda Console may only set spec.cluster.clusterRef.name, to ' + variables.clusterRef")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelinesAdmissionPolicyBinding" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not (get (fromJson (include "console.pipelinesViaServiceAccount" (dict "a" (list $state)))) "r")) (not $state.Values.pipelines.admissionPolicy.enabled)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "admissionregistration.k8s.io/v1" "kind" "ValidatingAdmissionPolicyBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.pipelinesClusterScopedName" (dict "a" (list $state "pipelines")))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "spec" (mustMergeOverwrite (dict) (dict "policyName" (get (fromJson (include "console.pipelinesClusterScopedName" (dict "a" (list $state "pipelines")))) "r") "validationActions" (list "Deny")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelinesClusterScopedName" -}}
{{- $state := (index .a 0) -}}
{{- $suffix := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s-%s-%s" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") $state.Namespace $suffix)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

