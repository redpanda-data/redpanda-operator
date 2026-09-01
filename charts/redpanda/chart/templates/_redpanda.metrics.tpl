{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/metrics.go" */ -}}

{{- define "redpanda.NewMetrics" -}}
{{- $dot := (index .a 0) -}}
{{- $values := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $coreMetricsEnabled := (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "enable_metrics_reporter" true $values.config.cluster))))) "r") -}}
{{- if (not $coreMetricsEnabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Enabled" false "ViaOperator" false "CloudEnvironment" "" "KubernetesVersion" "" "ChartVersion" "" "ClusterID" "") (dict))) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (not $values.logging.usageStats.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Enabled" false "ViaOperator" false "CloudEnvironment" "" "KubernetesVersion" "" "ChartVersion" "" "ClusterID" "") (dict))) | toJson -}}
{{- break -}}
{{- end -}}
{{- $kubeVersion := $dot.Capabilities.KubeVersion.Version -}}
{{- $metrics := (mustMergeOverwrite (dict "Enabled" false "ViaOperator" false "CloudEnvironment" "" "KubernetesVersion" "" "ChartVersion" "" "ClusterID" "") (dict "Enabled" true "KubernetesVersion" $kubeVersion "ChartVersion" $dot.Chart.Version)) -}}
{{- $_61_namespace_ok := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Namespace" "" "kube-system")))) "r") -}}
{{- $namespace := (index $_61_namespace_ok 0) -}}
{{- $ok := (index $_61_namespace_ok 1) -}}
{{- if $ok -}}
{{- $_ := (set $metrics "ClusterID" (toString $namespace.metadata.uid)) -}}
{{- end -}}
{{- if (contains "-gke" $kubeVersion) -}}
{{- $_ := (set $metrics "CloudEnvironment" "GCP") -}}
{{- else -}}{{- if (contains "-eks" $kubeVersion) -}}
{{- $_ := (set $metrics "CloudEnvironment" "AWS") -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $metrics) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.Metrics.EnvironmentVariables" -}}
{{- $m := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $m.Enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $deploymentType := "helm" -}}
{{- if $m.ViaOperator -}}
{{- $deploymentType = "operator" -}}
{{- end -}}
{{- $envvars := (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_VERSION" "value" $m.KubernetesVersion)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE" "value" $deploymentType)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CHART_VERSION" "value" $m.ChartVersion)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_OPERATOR_IMAGE_VERSION" "value" (printf `%s:%s` $pool.Statefulset.sideCars.image.repository $pool.Statefulset.sideCars.image.tag)))) -}}
{{- if (ne $m.ClusterID "") -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CLUSTER_ID" "value" $m.ClusterID)))) -}}
{{- end -}}
{{- if (ne $m.CloudEnvironment "") -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_ENVIRONMENT" "value" $m.CloudEnvironment)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $envvars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

