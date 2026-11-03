{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/metrics.go" */ -}}

{{- define "redpanda.MetricsEnvironmentVariables" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (get (fromJson (include "redpanda.metricsFromDir" (dict "a" (list $state)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_redpanda.MetricsDirEnv" (dict "a" (list)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.metricsValues" (dict "a" (list $state $pool)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.MetricsConfigMap" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.metricsFromDir" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "kind" "ConfigMap" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "_redpanda.MetricsConfigMapName" (dict "a" (list (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")))))) "r") "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "data" (get (fromJson (include "_redpanda.MetricsConfigMapData" (dict "a" (list (get (fromJson (include "redpanda.metricsValues" (dict "a" (list $state $pool)))) "r"))))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.metricsFromDir" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (and (get (fromJson (include "redpanda.metricsEnabled" (dict "a" (list $state)))) "r") (get (fromJson (include "_redpanda.MetricsFromDir" (dict "a" (list (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r"))))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.metricsEnabled" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $coreMetricsEnabled := (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "enable_metrics_reporter" true $state.Values.config.cluster))))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (and $coreMetricsEnabled $state.Values.logging.usageStats.enabled)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.metricsValues" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.metricsEnabled" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $deploymentType := "helm" -}}
{{- if $state.ViaOperator -}}
{{- $deploymentType = "operator" -}}
{{- end -}}
{{- $chartVersion := $state.Dot.Chart.Version -}}
{{- if (ne $state.OperatorVersion "") -}}
{{- $chartVersion = $state.OperatorVersion -}}
{{- end -}}
{{- $envvars := (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_VERSION" "value" $state.Dot.Capabilities.KubeVersion.Version)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE" "value" $deploymentType)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CHART_VERSION" "value" $chartVersion)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_OPERATOR_IMAGE_VERSION" "value" (printf `%s:%s` $pool.Statefulset.sideCars.image.repository $pool.Statefulset.sideCars.image.tag)))) -}}
{{- $_113_namespace_ok := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Namespace" "" "kube-system")))) "r") -}}
{{- $namespace := (index $_113_namespace_ok 0) -}}
{{- $ok := (index $_113_namespace_ok 1) -}}
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

