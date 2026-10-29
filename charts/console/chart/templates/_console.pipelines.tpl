{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/pipelines.go" */ -}}

{{- define "console.PipelinesTypes" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (dict)) (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (dict)) (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (dict)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIName" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.cleanForK8s" (dict "a" (list (printf "%s-pipeline-api" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r")))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelineAPISelectorLabels" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "app.kubernetes.io/name" (get (fromJson (include "console.cleanForK8s" (dict "a" (list (printf "%s-pipeline-api" (get (fromJson (include "console.RenderState.ChartName" (dict "a" (list $state)))) "r")))))) "r") "app.kubernetes.io/instance" $state.ReleaseName "app.kubernetes.io/component" "pipeline-api")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.validatePipelines" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (eq $state.Values.pipelines.image.tag "") -}}
{{- $_ := (fail "pipelines.image.tag is required: set it to the Redpanda Operator version that serves the pipeline API") -}}
{{- end -}}
{{- if (and (ne $state.Values.pipelines.clusterRef "") (eq $state.Values.pipelines.userRef "")) -}}
{{- $_ := (fail "pipelines.clusterRef requires pipelines.userRef: set it to a User with ACLs scoped to Console's pipelines") -}}
{{- end -}}
{{- if (eq (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r") "default") -}}
{{- $_ := (fail "pipelines.enabled requires a dedicated ServiceAccount for Console: set serviceAccount.create: true or serviceAccount.name") -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "console.consoleConfig" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.Values.config) | toJson -}}
{{- break -}}
{{- end -}}
{{- $redpandaConnect := (merge (dict) (dict) (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "map[%s]%s" "string" "interface {}") (dig "cloud" "redpandaConnect" (dict) $state.Values.config))))) "r")) -}}
{{- $_ := (set $redpandaConnect "enabled" true) -}}
{{- $_ := (set $redpandaConnect "address" (printf "%s.%s.svc:%d" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") $state.Namespace (8080 | int))) -}}
{{- $_ := (set $redpandaConnect "tokenFile" "/var/run/secrets/redpanda.com/pipeline-api/token") -}}
{{- $cloud := (merge (dict) (dict) (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "map[%s]%s" "string" "interface {}") (dig "cloud" (dict) $state.Values.config))))) "r")) -}}
{{- $_ := (set $cloud "redpandaConnect" $redpandaConnect) -}}
{{- $config := (merge (dict) (dict) $state.Values.config) -}}
{{- $_ := (set $config "cloud" $cloud) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $config) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelineAPITokenVolume" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "projected" (mustMergeOverwrite (dict "sources" (coalesce nil)) (dict "sources" (list (mustMergeOverwrite (dict) (dict "serviceAccountToken" (mustMergeOverwrite (dict "path" "") (dict "audience" "redpanda-pipeline-api" "expirationSeconds" ((3600 | int64) | int64) "path" "token"))))))))) (dict "name" "pipeline-api-token"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelineAPITokenVolumeMount" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "pipeline-api-token" "mountPath" "/var/run/secrets/redpanda.com/pipeline-api" "readOnly" true))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIServiceAccount" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "ServiceAccount")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIRole" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "rules" (coalesce nil)) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "Role")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "rules" (list (mustMergeOverwrite (dict "verbs" (coalesce nil)) (dict "apiGroups" (list "cluster.redpanda.com") "resources" (list "pipelines") "verbs" (list "get" "list" "create" "update" "delete"))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIRoleBinding" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "RoleBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "Role" "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r"))) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIClusterRoleBinding" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "roleRef" (dict "apiGroup" "" "kind" "" "name" "")) (mustMergeOverwrite (dict) (dict "apiVersion" "rbac.authorization.k8s.io/v1" "kind" "ClusterRoleBinding")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.pipelinesClusterScopedName" (dict "a" (list $state "pipeline-api")))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "roleRef" (mustMergeOverwrite (dict "apiGroup" "" "kind" "" "name" "") (dict "apiGroup" "rbac.authorization.k8s.io" "kind" "ClusterRole" "name" "system:auth-delegator")) "subjects" (list (mustMergeOverwrite (dict "kind" "" "name" "") (dict "kind" "ServiceAccount" "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace)))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIDeployment" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_ := (get (fromJson (include "console.validatePipelines" (dict "a" (list $state)))) "r") -}}
{{- $args := (list "pipeline-api" (printf "--namespace=%s" $state.Namespace) (printf "--listen-address=:%d" (8080 | int)) (printf "--allowed-caller=system:serviceaccount:%s:%s" $state.Namespace (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r"))) -}}
{{- if (ne $state.Values.pipelines.clusterRef "") -}}
{{- $args = (concat (default (list) $args) (list (printf "--cluster-ref=%s" $state.Values.pipelines.clusterRef))) -}}
{{- end -}}
{{- if (ne $state.Values.pipelines.userRef "") -}}
{{- $args = (concat (default (list) $args) (list (printf "--user-ref=%s" $state.Values.pipelines.userRef))) -}}
{{- end -}}
{{- $probe := (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "httpGet" (mustMergeOverwrite (dict "port" 0) (dict "path" "/healthz" "port" "grpc")))) (dict)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "apps/v1" "kind" "Deployment")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "spec" (mustMergeOverwrite (dict "selector" (coalesce nil) "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) (dict "replicas" ((1 | int) | int) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "console.pipelineAPISelectorLabels" (dict "a" (list $state)))) "r"))) "template" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "labels" (get (fromJson (include "console.pipelineAPISelectorLabels" (dict "a" (list $state)))) "r"))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "serviceAccountName" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "automountServiceAccountToken" true "imagePullSecrets" $state.Values.imagePullSecrets "securityContext" (mustMergeOverwrite (dict) (dict "runAsNonRoot" true "seccompProfile" (mustMergeOverwrite (dict "type" "") (dict "type" "RuntimeDefault")))) "containers" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "pipeline-api" "image" (printf "%s:%s" $state.Values.pipelines.image.repository $state.Values.pipelines.image.tag) "imagePullPolicy" $state.Values.pipelines.image.pullPolicy "command" (list "/redpanda-operator") "args" $args "ports" (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "grpc" "containerPort" (8080 | int) "protocol" "TCP"))) "readinessProbe" $probe "livenessProbe" $probe "resources" $state.Values.pipelines.resources "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false "readOnlyRootFilesystem" true "capabilities" (mustMergeOverwrite (dict) (dict "drop" (list "ALL"))))))))))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.PipelineAPIService" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.pipelines.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict) "status" (dict "loadBalancer" (dict))) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Service")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.PipelineAPIName" (dict "a" (list $state)))) "r") "namespace" $state.Namespace "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r"))) "spec" (mustMergeOverwrite (dict) (dict "type" "ClusterIP" "selector" (get (fromJson (include "console.pipelineAPISelectorLabels" (dict "a" (list $state)))) "r") "ports" (list (mustMergeOverwrite (dict "port" 0 "targetPort" 0) (dict "name" "grpc" "port" (8080 | int) "targetPort" "grpc" "protocol" "TCP" "appProtocol" "kubernetes.io/h2c")))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.pipelinesClusterScopedName" -}}
{{- $state := (index .a 0) -}}
{{- $suffix := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $hash := (trunc (8 | int) (sha256sum (printf "%s/%s" $state.Namespace $state.ReleaseName))) -}}
{{- $tail := (printf "-%s-%s" $hash $suffix) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s%s" (trimSuffix "-" (trunc ((sub (63 | int) ((get (fromJson (include "_shims.len" (dict "a" (list $tail)))) "r") | int)) | int) (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) $tail)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

