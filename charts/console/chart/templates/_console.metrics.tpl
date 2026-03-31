{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/metrics.go" */ -}}

{{- define "console.MetricsEnvironmentVariables" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $analyticsEnabled := (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "analytics" "enabled" true $state.Values.config))))) "r") -}}
{{- if (not $analyticsEnabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $deploymentType := "helm" -}}
{{- if $state.Metrics.ViaOperator -}}
{{- $deploymentType = "operator" -}}
{{- end -}}
{{- $envvars := (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE" "value" $deploymentType)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CHART_VERSION" "value" $state.Metrics.ChartVersion)) (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CONSOLE_IMAGE_VERSION" "value" (printf `%s:%s` $state.Values.image.repository (get (fromJson (include "console.consoleImageTag" (dict "a" (list $state)))) "r"))))) -}}
{{- if (ne $state.Metrics.KubernetesVersion "") -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_VERSION" "value" $state.Metrics.KubernetesVersion)))) -}}
{{- end -}}
{{- if (ne $state.Metrics.ClusterID "") -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_CLUSTER_ID" "value" $state.Metrics.ClusterID)))) -}}
{{- end -}}
{{- if (ne $state.Metrics.CloudEnvironment "") -}}
{{- $envvars = (concat (default (list) $envvars) (list (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_METRICS_K8S_ENVIRONMENT" "value" $state.Metrics.CloudEnvironment)))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $envvars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.consoleImageTag" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tag := $state.Values.image.tag -}}
{{- if (eq $tag "") -}}
{{- $tag = "v3.7.0" -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $tag) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

