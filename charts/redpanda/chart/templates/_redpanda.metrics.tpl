{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/metrics.go" */ -}}

{{- define "redpanda.MetricsEnvironmentVariables" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $coreMetricsEnabled := (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "enable_metrics_reporter" true $state.Values.config.cluster))))) "r") -}}
{{- if (not $coreMetricsEnabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not $state.Values.logging.usageStats.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $deploymentType := "helm" -}}
{{- if $state.ViaOperator -}}
{{- $deploymentType = "operator" -}}
{{- end -}}
{{- $envvars := (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_VERSION" "value" $state.Dot.Capabilities.KubeVersion.Version)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE" "value" $deploymentType)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CHART_VERSION" "value" $state.Dot.Chart.Version)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_OPERATOR_IMAGE_VERSION" "value" (printf `%s:%s` $pool.Statefulset.sideCars.image.repository $pool.Statefulset.sideCars.image.tag)))) -}}
{{- $_69_namespace_ok := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Namespace" "" "kube-system")))) "r") -}}
{{- $namespace := (index $_69_namespace_ok 0) -}}
{{- $ok := (index $_69_namespace_ok 1) -}}
{{- if $ok -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CLUSTER_ID" "value" (toString $namespace.metadata.uid))))) -}}
{{- end -}}
{{- if (not (empty $state.CloudEnvironment)) -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_ENVIRONMENT" "value" $state.CloudEnvironment)))) -}}
{{- else -}}
{{- if (contains "-gke" $state.Dot.Capabilities.KubeVersion.Version) -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_ENVIRONMENT" "value" "GCP")))) -}}
{{- else -}}{{- if (contains "-eks" $state.Dot.Capabilities.KubeVersion.Version) -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_ENVIRONMENT" "value" "AWS")))) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $envvars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

