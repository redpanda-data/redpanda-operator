{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/chart/chart.go" */ -}}

{{- define "chart.Render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $state := (get (fromJson (include "chart.DotToState" (dict "a" (list $dot)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.Render" (dict "a" (list $state)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.DotToState" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $templater := (mustMergeOverwrite (dict "Dot" (coalesce nil) "FauxDot" (coalesce nil)) (dict "Dot" $dot "FauxDot" (get (fromJson (include "chart.newFauxDot" (dict "a" (list $dot)))) "r"))) -}}
{{- if (eq $values.secret.authentication.jwtSigningKey "") -}}
{{- $_ := (set $values.secret.authentication "jwtSigningKey" (randAlphaNum (32 | int))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "ReleaseName" "" "Namespace" "" "Template" (coalesce nil) "CommonLabels" (coalesce nil) "Values" (dict "replicaCount" 0 "nameOverride" "" "commonLabels" (coalesce nil) "fullnameOverride" "" "image" (dict "registry" "" "repository" "" "pullPolicy" "" "tag" "") "imagePullSecrets" (coalesce nil) "automountServiceAccountToken" false "serviceAccount" (dict "create" false "automountServiceAccountToken" false "annotations" (coalesce nil) "name" "") "annotations" (coalesce nil) "podAnnotations" (coalesce nil) "podLabels" (coalesce nil) "podSecurityContext" (dict) "securityContext" (dict) "service" (dict "type" "" "port" 0 "annotations" (coalesce nil)) "ingress" (dict "enabled" false "annotations" (coalesce nil) "hosts" (coalesce nil) "tls" (coalesce nil)) "resources" (dict) "autoscaling" (dict "enabled" false "minReplicas" 0 "maxReplicas" 0 "targetCPUUtilizationPercentage" (coalesce nil)) "nodeSelector" (coalesce nil) "tolerations" (coalesce nil) "affinity" (dict) "topologySpreadConstraints" (coalesce nil) "priorityClassName" "" "config" (coalesce nil) "extraEnv" (coalesce nil) "extraEnvFrom" (coalesce nil) "extraVolumes" (coalesce nil) "extraVolumeMounts" (coalesce nil) "extraContainers" (coalesce nil) "extraContainerPorts" (coalesce nil) "initContainers" (dict "extraInitContainers" (coalesce nil)) "secretMounts" (coalesce nil) "secret" (dict "create" false "kafka" (dict) "authentication" (dict "jwtSigningKey" "" "oidc" (dict)) "license" "" "redpanda" (dict "adminApi" (dict)) "serde" (dict) "schemaRegistry" (dict)) "livenessProbe" (dict) "readinessProbe" (dict) "configmap" (dict "create" false) "deployment" (dict "create" false) "strategy" (dict))) (dict "ReleaseName" $dot.Release.Name "Namespace" $dot.Release.Namespace "Values" $values "Template" (list "chart.templater.Template" $templater) "CommonLabels" (dict "helm.sh/chart" (get (fromJson (include "chart.ChartLabel" (dict "a" (list $dot)))) "r") "app.kubernetes.io/managed-by" $dot.Release.Service "app.kubernetes.io/version" $dot.Chart.AppVersion)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.templater.Template" -}}
{{- $t := (index .a 0) -}}
{{- $tpl := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (tpl $tpl $t.FauxDot)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.newFauxDot" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $clone := (dict) -}}
{{- range $key, $value := $dot.Values.AsMap -}}
{{- $_ := (set $clone $key $value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $clone "AsMap" $dot.Values.AsMap) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "Values" (coalesce nil) "Chart" (dict "Name" "" "Version" "" "AppVersion" "") "Release" (dict "Name" "" "Namespace" "" "Service" "" "IsUpgrade" false "IsInstall" false) "Template" (dict "Name" "" "BasePath" "")) (dict "Values" $clone "Release" $dot.Release "Template" $dot.Template "Chart" (mustMergeOverwrite (dict "Name" "" "Version" "" "AppVersion" "") (dict "Name" $dot.Chart.Name "AppVersion" $dot.Chart.AppVersion "Version" $dot.Chart.Version))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.Name" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.RenderState.ChartName" (dict "a" (list (get (fromJson (include "chart.DotToState" (dict "a" (list $dot)))) "r"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.Fullname" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list (get (fromJson (include "chart.DotToState" (dict "a" (list $dot)))) "r"))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.ChartLabel" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $chart := (printf "%s-%s" $dot.Chart.Name $dot.Chart.Version) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "chart.cleanForK8s" (dict "a" (list (replace "+" "_" $chart))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "chart.cleanForK8s" -}}
{{- $s := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (trimSuffix "-" (trunc (63 | int) $s))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

