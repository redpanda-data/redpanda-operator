{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/files/files.go" */ -}}

{{- define "files.Files" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list ($dot.Files.Get "hello.txt") ($dot.Files.GetBytes "hello.txt") ($dot.Files.Lines "something.yaml") ($dot.Files.Get "doesntexist") ($dot.Files.GetBytes "doesntexist") ($dot.Files.Lines "doesntexist"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

