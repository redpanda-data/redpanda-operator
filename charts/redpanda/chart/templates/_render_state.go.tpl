{{- /* Generated from "render_state.go" */ -}}

{{- define "redpanda.RenderState.FetchBootstrapUser" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (or (eq (toJson $r.Values.auth.sasl) "null") (not $r.Values.auth.sasl.enabled)) (ne (toJson $r.Values.auth.sasl.bootstrapUser.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secretName := (printf "%s-bootstrap-user" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r")) -}}
{{- $_98_existing_1_ok_2 := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Secret" $r.Release.Namespace $secretName)))) "r") -}}
{{- $existing_1 := (index $_98_existing_1_ok_2 0) -}}
{{- $ok_2 := (index $_98_existing_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $_ := (set $existing_1 "immutable" true) -}}
{{- $_ := (set $r "BootstrapUserSecret" $existing_1) -}}
{{- $selector := (get (fromJson (include "redpanda.BootstrapUser.SecretKeySelector" (dict "a" (list $r.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $_115_data_3_found_4 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $existing_1.data $selector.key (coalesce nil))))) "r") -}}
{{- $data_3 := (index $_115_data_3_found_4 0) -}}
{{- $found_4 := (index $_115_data_3_found_4 1) -}}
{{- if $found_4 -}}
{{- $_ := (set $r "BootstrapUserPassword" (toString $data_3)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RenderState.FetchStatefulSetPodSelector" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if $r.Release.IsUpgrade -}}
{{- $_130_existing_5_ok_6 := (get (fromJson (include "_shims.lookup" (dict "a" (list "apps/v1" "StatefulSet" $r.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $existing_5 := (index $_130_existing_5_ok_6 0) -}}
{{- $ok_6 := (index $_130_existing_5_ok_6 1) -}}
{{- if (and $ok_6 (gt ((get (fromJson (include "_shims.len" (dict "a" (list $existing_5.spec.template.metadata.labels)))) "r") | int) (0 | int))) -}}
{{- $_ := (set $r "StatefulSetPodLabels" $existing_5.spec.template.metadata.labels) -}}
{{- $_ := (set $r "StatefulSetSelector" $existing_5.spec.selector.matchLabels) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

