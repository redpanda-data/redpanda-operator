{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/aaacommon/common.go" */ -}}

{{- define "aaacommon.SharedConstant" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" "You've imported the aaacommon package!") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

