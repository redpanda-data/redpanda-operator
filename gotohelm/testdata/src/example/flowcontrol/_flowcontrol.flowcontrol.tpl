{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "example.com/example/flowcontrol/flowcontrol.go" */ -}}

{{- define "flowcontrol.FlowControl" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "earlyReturn" (get (fromJson (include "flowcontrol.earlyReturn" (dict "a" (list $dot)))) "r") "ifElse" (get (fromJson (include "flowcontrol.ifElse" (dict "a" (list $dot)))) "r") "sliceRanges" (get (fromJson (include "flowcontrol.sliceRanges" (dict "a" (list $dot)))) "r") "mapRanges" (get (fromJson (include "flowcontrol.mapRanges" (dict "a" (list $dot)))) "r") "intBinaryExprs" (get (fromJson (include "flowcontrol.intBinaryExprs" (dict "a" (list)))) "r") "blockScoping" (get (fromJson (include "flowcontrol.blockScoping" (dict "a" (list)))) "r") "switches" (get (fromJson (include "flowcontrol.switches" (dict "a" (list $dot)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.earlyReturn" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_30_b_1_ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $dot.Values "boolean" (coalesce nil))))) "r") -}}
{{- $b_1 := (index $_30_b_1_ok_2 0) -}}
{{- $ok_2 := (index $_30_b_1_ok_2 1) -}}
{{- if (and $ok_2 (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" $b_1)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "Early Returns work!") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "Should have returned early") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.ifElse" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_40_oneToFour_ok := (get (fromJson (include "_shims.asintegral" (dict "a" (list (index $dot.Values "oneToFour"))))) "r") -}}
{{- $oneToFour := ((index $_40_oneToFour_ok 0) | int) -}}
{{- $ok := (index $_40_oneToFour_ok 1) -}}
{{- if (not $ok) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "oneToFour not specified!") | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq $oneToFour (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "It's 1") | toJson -}}
{{- break -}}
{{- else -}}{{- if (eq $oneToFour (2 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "It's 2") | toJson -}}
{{- break -}}
{{- else -}}{{- if (eq $oneToFour (3 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "It's 3") | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" "It's 4") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "unreachable") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.sliceRanges" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_58_intsAny_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $dot.Values "ints" (coalesce nil))))) "r") -}}
{{- $intsAny := (index $_58_intsAny_ok 0) -}}
{{- $ok := (index $_58_intsAny_ok 1) -}}
{{- if (not $ok) -}}
{{- $intsAny = (list) -}}
{{- end -}}
{{- $ints := (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "[]%s" "interface {}") $intsAny)))) "r") -}}
{{- $sumOfIndexes := (0 | int) -}}
{{- range $i, $_ := $ints -}}
{{- $sumOfIndexes = ((add $sumOfIndexes $i) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $continuesWork := true -}}
{{- range $_, $_ := $ints -}}
{{- continue -}}
{{- $continuesWork = false -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $breaksWork := true -}}
{{- range $_, $_ := $ints -}}
{{- break -}}
{{- $breaksWork = false -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $sumOfIndexes $continuesWork $breaksWork)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.mapRanges" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict "1" (1 | int) "2" (2 | int) "3" (3 | int)) -}}
{{- range $k, $_ := $m -}}
{{- $_ = $k -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sum := (0 | int) -}}
{{- range $_, $v := $m -}}
{{- $sum = ((add $sum $v) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $sum)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.switches" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_107_oneToFour_ok := (get (fromJson (include "_shims.asintegral" (dict "a" (list (index $dot.Values "oneToFour"))))) "r") -}}
{{- $oneToFour := ((index $_107_oneToFour_ok 0) | int) -}}
{{- $ok := (index $_107_oneToFour_ok 1) -}}
{{- if (not $ok) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "tagged" (get (fromJson (include "flowcontrol.tagged" (dict "a" (list $oneToFour)))) "r") "tagless" (get (fromJson (include "flowcontrol.tagless" (dict "a" (list $oneToFour)))) "r") "defaultFirst" (get (fromJson (include "flowcontrol.defaultFirst" (dict "a" (list $oneToFour)))) "r") "noDefault" (get (fromJson (include "flowcontrol.noDefault" (dict "a" (list $oneToFour)))) "r") "onlyDefault" (get (fromJson (include "flowcontrol.onlyDefault" (dict "a" (list)))) "r") "nested" (get (fromJson (include "flowcontrol.nested" (dict "a" (list $oneToFour)))) "r") "initShadows" (get (fromJson (include "flowcontrol.initShadows" (dict "a" (list $oneToFour)))) "r") "switchInit" (get (fromJson (include "flowcontrol.switchInit" (dict "a" (list $oneToFour)))) "r") "inRange" (get (fromJson (include "flowcontrol.inRange" (dict "a" (list $oneToFour)))) "r") "returns" (get (fromJson (include "flowcontrol.returns" (dict "a" (list $oneToFour)))) "r") "onString" (get (fromJson (include "flowcontrol.onString" (dict "a" (list $oneToFour)))) "r") "commented" (get (fromJson (include "flowcontrol.commented" (dict "a" (list $oneToFour)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.tagged" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (or (eq $_tmp_0 (1 | int)) (eq $_tmp_0 (2 | int))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "low") | toJson -}}
{{- break -}}
{{- else -}}{{- if (eq $_tmp_0 (3 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "three") | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" "high") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.tagless" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (lt $x (2 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "under") | toJson -}}
{{- break -}}
{{- else -}}{{- if (lt $x (4 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "middle") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "over") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.defaultFirst" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (eq $_tmp_0 (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "one") | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" "other") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.noDefault" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := "unset" -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (eq $_tmp_0 (1 | int)) -}}
{{- $out = "one" -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.onlyDefault" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" "only") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.nested" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (or (eq $_tmp_0 (1 | int)) (eq $_tmp_0 (2 | int))) -}}
{{- if true -}}
{{- $_tmp_0 := ((mul $x (10 | int)) | int) -}}
{{- if (eq $_tmp_0 (10 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "1") | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" "2") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- else -}}{{- if (eq $_tmp_0 (3 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "3") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "many") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.initShadows" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $x := ((mul $x (10 | int)) | int) -}}
{{- $_tmp_0 := $x -}}
{{- if (eq $_tmp_0 (10 | int)) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $x)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.switchInit" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $m := (dict "a" (1 | int)) -}}
{{- if true -}}
{{- $_231_v_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $m "a" 0)))) "r") -}}
{{- $v := ((index $_231_v_ok 0) | int) -}}
{{- $ok := (index $_231_v_ok 1) -}}
{{- $_tmp_0 := $v -}}
{{- if (eq $_tmp_0 $x) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "match" $ok)) | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "miss" $ok)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.inRange" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (coalesce nil) -}}
{{- range $_, $i := (list (1 | int) (2 | int) (3 | int) (4 | int)) -}}
{{- if true -}}
{{- $_tmp_0 := $i -}}
{{- if (eq $_tmp_0 $x) -}}
{{- continue -}}
{{- end -}}
{{- end -}}
{{- $out = (concat (default (list) $out) (list $i)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.returns" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (eq $_tmp_0 (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "one") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "other") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.onString" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := (printf "%d" $x) -}}
{{- if (eq $_tmp_0 "1") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "one") | toJson -}}
{{- break -}}
{{- else -}}{{- if (or (eq $_tmp_0 "2") (eq $_tmp_0 "3")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "few") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "many") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.commented" -}}
{{- $x := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if true -}}
{{- $_tmp_0 := $x -}}
{{- if (eq $_tmp_0 (1 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "one") | toJson -}}
{{- break -}}
{{- else -}}{{- if (eq $_tmp_0 (2 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "two") | toJson -}}
{{- break -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" "other") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.blockScoping" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $x := (1 | int) -}}
{{- if true -}}
{{- $x := (2 | int) -}}
{{- $_ = $x -}}
{{- end -}}
{{- if true -}}
{{- $x := (3 | int) -}}
{{- $_ = $x -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $x)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "flowcontrol.intBinaryExprs" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $x := (1 | int) -}}
{{- $y := (2 | int) -}}
{{- $z := (3 | int) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $z ((sub $x $y) | int) ((add $x $y) | int) ((div $x $y) | int) ((mul $x $y) | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

