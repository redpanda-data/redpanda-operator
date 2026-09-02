{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/astrewrites/astrewrites.go" */ -}}

{{- define "astrewrites.ASTRewrites" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "astrewrites.mvrs" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict) -}}
{{- $a := $m -}}
{{- $_26_x_y := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "1" 0)))) "r") -}}
{{- $x := ((index $_26_x_y 0) | int) -}}
{{- $y := (index $_26_x_y 1) -}}
{{- $_ = $x -}}
{{- $_ = $y -}}
{{- $_32_x_y := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "int") $a (coalesce nil))))) "r") -}}
{{- $x := (index $_32_x_y 0) -}}
{{- $y := (index $_32_x_y 1) -}}
{{- $_ = $x -}}
{{- $_ = $y -}}
{{- $_38_x__ := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "int") $a (coalesce nil))))) "r") -}}
{{- $x := (index $_38_x__ 0) -}}
{{- $_ := (index $_38_x__ 1) -}}
{{- $_ = $x -}}
{{- $_43___x := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "int") $a (coalesce nil))))) "r") -}}
{{- $_ := (index $_43___x 0) -}}
{{- $x := (index $_43___x 1) -}}
{{- $_ = $x -}}
{{- $_48____ := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "int") $a (coalesce nil))))) "r") -}}
{{- $_ = (index $_48____ 0) -}}
{{- $_ = (index $_48____ 1) -}}
{{- $_52_a_b_c := (get (fromJson (include "astrewrites.mvr3" (dict "a" (list)))) "r") -}}
{{- $a := (index $_52_a_b_c 0) -}}
{{- $b := (index $_52_a_b_c 1) -}}
{{- $c := ((index $_52_a_b_c 2) | int) -}}
{{- $_ = $a -}}
{{- $_ = $b -}}
{{- $_ = $c -}}
{{- $a := "a" -}}
{{- $b := "b" -}}
{{- $_tmp_0 := $b -}}
{{- $_tmp_1 := $a -}}
{{- $a = $_tmp_0 -}}
{{- $b = $_tmp_1 -}}
{{- $_ = $a -}}
{{- $_ = $b -}}
{{- $m := (dict) -}}
{{- $_75_x_y := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "" (dict))))) "r") -}}
{{- $x := (index $_75_x_y 0) -}}
{{- $y := (index $_75_x_y 1) -}}
{{- $_ = $x -}}
{{- $_ = $y -}}
{{- end -}}
{{- end -}}

{{- define "astrewrites.dictTest" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict) -}}
{{- $_85___ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "" 0)))) "r") -}}
{{- $_ := ((index $_85___ok 0) | int) -}}
{{- $ok := (index $_85___ok 1) -}}
{{- $_ = $ok -}}
{{- end -}}
{{- end -}}

{{- define "astrewrites.typeTest" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict) -}}
{{- $_92___ok := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "string") $m (coalesce nil))))) "r") -}}
{{- $_ := (index $_92___ok 0) -}}
{{- $ok := (index $_92___ok 1) -}}
{{- $_ = $ok -}}
{{- $_95____ := (get (fromJson (include "_shims.typetest" (dict "a" (list (printf "map[%s]%s" "string" "int") $m (coalesce nil))))) "r") -}}
{{- $_ = (index $_95____ 0) -}}
{{- $_ = (index $_95____ 1) -}}
{{- end -}}
{{- end -}}

{{- define "astrewrites.ifHoisting" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict "1" (1 | int)) -}}
{{- $_101___ok_1 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "2" 0)))) "r") -}}
{{- $_ := ((index $_101___ok_1 0) | int) -}}
{{- $ok_1 := (index $_101___ok_1 1) -}}
{{- $_102___ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "3" 0)))) "r") -}}
{{- $_ := ((index $_102___ok_2 0) | int) -}}
{{- $ok_2 := (index $_102___ok_2 1) -}}
{{- $_103___ok_3 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "4" 0)))) "r") -}}
{{- $_ := ((index $_103___ok_3 0) | int) -}}
{{- $ok_3 := (index $_103___ok_3 1) -}}
{{- $_104___ok_4 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "5" 0)))) "r") -}}
{{- $_ := ((index $_104___ok_4 0) | int) -}}
{{- $ok_4 := (index $_104___ok_4 1) -}}
{{- if $ok_1 -}}
{{- else -}}{{- if $ok_2 -}}
{{- else -}}{{- if $ok_3 -}}
{{- else -}}{{- if $ok_4 -}}
{{- else -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "astrewrites.mvr3" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list 0 true (3 | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

