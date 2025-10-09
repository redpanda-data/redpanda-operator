{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/pkg/chartutil/chartutil.go" */ -}}

{{- define "chartutil.ParseFlags" -}}
{{- $args := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $parsed := (dict) -}}
{{- $i := -1 -}}
{{- range $_, $_ := $args -}}
{{- $i = ((add $i (1 | int)) | int) -}}
{{- if (ge $i ((get (fromJson (include "_shims.len" (dict "a" (list $args)))) "r") | int)) -}}
{{- break -}}
{{- end -}}
{{- if (not (hasPrefix "-" (index $args $i))) -}}
{{- continue -}}
{{- end -}}
{{- $flag := (index $args $i) -}}
{{- $spl := (mustRegexSplit " |=" $flag (2 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $spl)))) "r") | int) (2 | int)) -}}
{{- $_ := (set $parsed (index $spl (0 | int)) (index $spl (1 | int))) -}}
{{- continue -}}
{{- end -}}
{{- if (and (lt ((add $i (1 | int)) | int) ((get (fromJson (include "_shims.len" (dict "a" (list $args)))) "r") | int)) (not (hasPrefix "-" (index $args ((add $i (1 | int)) | int))))) -}}
{{- $_ := (set $parsed $flag (index $args ((add $i (1 | int)) | int))) -}}
{{- $i = ((add $i (1 | int)) | int) -}}
{{- continue -}}
{{- end -}}
{{- $_ := (set $parsed $flag "") -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $parsed) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

