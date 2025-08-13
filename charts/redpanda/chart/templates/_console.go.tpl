{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/console.tpl.go" */ -}}

{{- define "redpanda.consoleChartIntegration" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.enabled true)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $consoleState := (get (fromJson (include "chart.DotToState" (dict "a" (list (index $state.Dot.Subcharts "console"))))) "r") -}}
{{- $staticCfg := (get (fromJson (include "redpanda.RenderState.AsStaticConfigSource" (dict "a" (list $state)))) "r") -}}
{{- $overlay := (get (fromJson (include "console.StaticConfigurationSourceToPartialRenderValues" (dict "a" (list $staticCfg)))) "r") -}}
{{- $_ := (set $consoleState.Values.configmap "create" true) -}}
{{- $_ := (set $consoleState.Values.deployment "create" true) -}}
{{- $_ := (set $consoleState.Values "extraEnv" (concat (default (list) $overlay.extraEnv) (default (list) $consoleState.Values.extraEnv))) -}}
{{- $_ := (set $consoleState.Values "extraVolumes" (concat (default (list) $overlay.extraVolumes) (default (list) $consoleState.Values.extraVolumes))) -}}
{{- $_ := (set $consoleState.Values "extraVolumeMounts" (concat (default (list) $overlay.extraVolumeMounts) (default (list) $consoleState.Values.extraVolumeMounts))) -}}
{{- $_ := (set $consoleState.Values "config" (merge (dict) $consoleState.Values.config $overlay.config)) -}}
{{- if (ne (toJson $state.Values.enterprise.licenseSecretRef) "null") -}}
{{- $_ := (set $consoleState.Values "licenseSecretRef" $state.Values.enterprise.licenseSecretRef) -}}
{{- end -}}
{{- $license_1 := $state.Values.enterprise.license -}}
{{- if (and (ne $license_1 "") (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.console.secret.create false)))) "r"))) -}}
{{- $_ := (set $consoleState.Values.secret "create" true) -}}
{{- $_ := (set $consoleState.Values.secret "license" $license_1) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "console.Secret" (dict "a" (list $consoleState)))) "r") (get (fromJson (include "console.ConfigMap" (dict "a" (list $consoleState)))) "r") (get (fromJson (include "console.Deployment" (dict "a" (list $consoleState)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

