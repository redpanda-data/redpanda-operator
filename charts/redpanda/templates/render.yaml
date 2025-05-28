{{- /* Generated from "render.go" */ -}}

{{- define "redpanda.checkVersion" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (and (not (get (fromJson (include "redpanda.RedpandaAtLeast_22_2_0" (dict "a" (list $dot)))) "r")) (not $values.force)) -}}
{{- $sv := (get (fromJson (include "redpanda.semver" (dict "a" (list $dot)))) "r") -}}
{{- $_ := (fail (printf "Error: The Redpanda version (%s) is no longer supported \nTo accept this risk, run the upgrade again adding `--force=true`\n" $sv)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

