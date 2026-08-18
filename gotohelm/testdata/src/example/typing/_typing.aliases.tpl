{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/typing/aliases.go" */ -}}

{{- define "typing.Object.Describe" -}}
{{- $m := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" $m.Key) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "typing.aliases" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $size := 0 -}}
{{- $stamp := (coalesce nil) -}}
{{- $described := (mustMergeOverwrite (dict "Key" "" "with_tag" 0) (mustMergeOverwrite (dict "Key" "" "with_tag" 0) (dict "Key" "described")) (dict)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $size $stamp (mustMergeOverwrite (dict "Key" "" "with_tag" 0) (dict)) (mustMergeOverwrite (dict "Key" "" "with_tag" 0) (mustMergeOverwrite (dict "Key" "" "with_tag" 0) (dict "Key" "aliased")) (dict "size" ((64 | int64) | int64))) (get (fromJson (include "typing.Object.Describe" (dict "a" (list (deepCopy $described))))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

