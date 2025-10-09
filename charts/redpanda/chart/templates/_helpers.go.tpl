{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/helpers.go" */ -}}

{{- define "redpanda.ChartLabel" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list (replace "+" "_" (printf "%s-%s" $state.Chart.Name $state.Chart.Version)))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.Name" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $override_1 := $state.Values.nameOverride -}}
{{- if (ne $override_1 "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list $override_1)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list $state.Chart.Name)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.Fullname" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $override_2 := $state.Values.fullnameOverride -}}
{{- if (ne $override_2 "") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list $override_2)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list $state.Release.Name)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.FullLabels" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $labels := (dict) -}}
{{- if (ne (toJson $state.Values.commonLabels) "null") -}}
{{- $labels = $state.Values.commonLabels -}}
{{- end -}}
{{- $defaults := (dict "helm.sh/chart" (get (fromJson (include "redpanda.ChartLabel" (dict "a" (list $state)))) "r") "app.kubernetes.io/name" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") "app.kubernetes.io/instance" $state.Release.Name "app.kubernetes.io/managed-by" $state.Release.Service "app.kubernetes.io/component" (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $labels $defaults)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.Tag" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tag := (toString $state.Values.image.tag) -}}
{{- if (eq $tag "") -}}
{{- $tag = $state.Chart.AppVersion -}}
{{- end -}}
{{- $pattern := "^v(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$" -}}
{{- if (not (regexMatch $pattern $tag)) -}}
{{- $_ := (fail "image.tag must start with a 'v' and be a valid semver") -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $tag) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ServiceName" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (ne (toJson $state.Values.service) "null") (ne (toJson $state.Values.service.name) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.cleanForK8s" (dict "a" (list $state.Values.service.name)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.InternalDomain" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $service := (get (fromJson (include "redpanda.ServiceName" (dict "a" (list $state)))) "r") -}}
{{- $ns := $state.Release.Namespace -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s.%s.svc.%s" $service $ns $state.Values.clusterDomain)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.DefaultMounts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "base-config" "mountPath" "/etc/redpanda")))) (default (list) (get (fromJson (include "redpanda.CommonMounts" (dict "a" (list $state)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.CommonMounts" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $mounts := (list) -}}
{{- $sasl_3 := $state.Values.auth.sasl -}}
{{- if (and $sasl_3.enabled (ne $sasl_3.secretRef "")) -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "users" "mountPath" "/etc/secrets/users" "readOnly" true)))) -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseServerCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $cert := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (get (fromJson (include "redpanda.TLSCert.ServerVolumeName" (dict "a" (list $cert $name)))) "r") "mountPath" (get (fromJson (include "redpanda.TLSCert.ServerMountPoint" (dict "a" (list $cert $name)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseClientCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $cert := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- $mounts = (concat (default (list) $mounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (get (fromJson (include "redpanda.TLSCert.ClientVolumeName" (dict "a" (list $cert $name)))) "r") "mountPath" (get (fromJson (include "redpanda.TLSCert.ClientMountPoint" (dict "a" (list $cert $name)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $mounts) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.DefaultVolumes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r"))) (dict)))) (dict "name" "base-config")))) (default (list) (get (fromJson (include "redpanda.CommonVolumes" (dict "a" (list $state)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.CommonVolumes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volumes := (list) -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseServerCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $cert := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (get (fromJson (include "redpanda.TLSCert.ServerSecretName" (dict "a" (list $cert $state $name)))) "r") "defaultMode" (0o440 | int))))) (dict "name" (get (fromJson (include "redpanda.TLSCert.ServerVolumeName" (dict "a" (list $cert $name)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $name := (get (fromJson (include "redpanda.Listeners.InUseClientCerts" (dict "a" (list $state.Values.listeners $state.Values.tls)))) "r") -}}
{{- $cert := (get (fromJson (include "redpanda.TLSCertMap.MustGet" (dict "a" (list (deepCopy $state.Values.tls.certs) $name)))) "r") -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (get (fromJson (include "redpanda.TLSCert.ClientSecretName" (dict "a" (list $cert $state $name)))) "r") "defaultMode" (0o440 | int))))) (dict "name" (get (fromJson (include "redpanda.TLSCert.ClientVolumeName" (dict "a" (list $cert $name)))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $sasl_4 := $state.Values.auth.sasl -}}
{{- if (and $sasl_4.enabled (ne $sasl_4.secretRef "")) -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" $sasl_4.secretRef)))) (dict "name" "users")))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $volumes) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_22_2_0" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=22.2.0-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_22_3_0" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=22.3.0-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_23_1_1" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=23.1.1-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_23_1_2" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=23.1.2-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_22_3_atleast_22_3_13" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=22.3.13-0,<22.4")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_22_2_atleast_22_2_10" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=22.2.10-0,<22.3")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_23_2_1" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=23.2.1-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAtLeast_23_3_0" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.redpandaAtLeast" (dict "a" (list $state ">=23.3.0-0 || <0.0.1-0")))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.redpandaAtLeast" -}}
{{- $state := (index .a 0) -}}
{{- $constraint := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $version := (trimPrefix "v" (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")) -}}
{{- $_277_result_err := (list (semverCompare $constraint $version) nil) -}}
{{- $result := (index $_277_result_err 0) -}}
{{- $err := (index $_277_result_err 1) -}}
{{- if (ne (toJson $err) "null") -}}
{{- $_ := (fail $err) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.cleanForK8s" -}}
{{- $in := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (trimSuffix "-" (trunc (63 | int) $in))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StructuredTpl" -}}
{{- $state := (index .a 0) -}}
{{- $in := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $untyped := $in -}}
{{- $expanded := (get (fromJson (include "redpanda.recursiveTpl" (dict "a" (list $state $untyped)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $expanded)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.recursiveTpl" -}}
{{- $state := (index .a 0) -}}
{{- $data := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $kind := (kindOf $data) -}}
{{- if (eq $kind "map") -}}
{{- $m := (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "map[%s]%s" "string" "interface {}") $data)))) "r") -}}
{{- range $key, $value := $m -}}
{{- $_ := (set $m $key (get (fromJson (include "redpanda.recursiveTpl" (dict "a" (list $state $value)))) "r")) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $m) | toJson -}}
{{- break -}}
{{- else -}}{{- if (eq $kind "slice") -}}
{{- $s := (get (fromJson (include "_shims.typeassertion" (dict "a" (list (printf "[]%s" "interface {}") $data)))) "r") -}}
{{- $out := (coalesce nil) -}}
{{- range $i, $_ := $s -}}
{{- $out = (concat (default (list) $out) (list (get (fromJson (include "redpanda.recursiveTpl" (dict "a" (list $state (index $s $i))))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- else -}}{{- if (and (eq $kind "string") (contains "{{" (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $data)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (tpl (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $data)))) "r") $state.Dot)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $data) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.StrategicMergePatch" -}}
{{- $overrides := (index .a 0) -}}
{{- $original := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $overridesClone := (fromJson (toJson $overrides)) -}}
{{- $overrides = (merge (dict) $overridesClone) -}}
{{- $overrideSpec := $overrides.spec -}}
{{- if (eq (toJson $overrideSpec) "null") -}}
{{- $overrideSpec = (mustMergeOverwrite (dict) (dict)) -}}
{{- end -}}
{{- $merged := (merge (dict) (mustMergeOverwrite (dict) (dict "metadata" (mustMergeOverwrite (dict) (dict "labels" $overrides.labels "annotations" $overrides.annotations)) "spec" $overrideSpec)) $original) -}}
{{- $_ := (set $merged.spec "initContainers" (get (fromJson (include "redpanda.mergeSliceBy" (dict "a" (list $original.spec.initContainers $overrideSpec.initContainers "name" (list "redpanda.mergeContainer"))))) "r")) -}}
{{- $_ := (set $merged.spec "containers" (get (fromJson (include "redpanda.mergeSliceBy" (dict "a" (list $original.spec.containers $overrideSpec.containers "name" (list "redpanda.mergeContainer"))))) "r")) -}}
{{- $_ := (set $merged.spec "volumes" (get (fromJson (include "redpanda.mergeSliceBy" (dict "a" (list $original.spec.volumes $overrideSpec.volumes "name" (list "redpanda.mergeVolume"))))) "r")) -}}
{{- if (eq (toJson $merged.metadata.labels) "null") -}}
{{- $_ := (set $merged.metadata "labels" (dict)) -}}
{{- end -}}
{{- if (eq (toJson $merged.metadata.annotations) "null") -}}
{{- $_ := (set $merged.metadata "annotations" (dict)) -}}
{{- end -}}
{{- if (eq (toJson $merged.spec.nodeSelector) "null") -}}
{{- $_ := (set $merged.spec "nodeSelector" (dict)) -}}
{{- end -}}
{{- if (eq (toJson $merged.spec.tolerations) "null") -}}
{{- $_ := (set $merged.spec "tolerations" (list)) -}}
{{- end -}}
{{- if (eq (toJson $merged.spec.imagePullSecrets) "null") -}}
{{- $_ := (set $merged.spec "imagePullSecrets" (list)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $merged) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.mergeSliceBy" -}}
{{- $original := (index .a 0) -}}
{{- $override := (index .a 1) -}}
{{- $mergeKey := (index .a 2) -}}
{{- $mergeFunc := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $originalKeys := (dict) -}}
{{- $overrideByKey := (dict) -}}
{{- range $_, $el := $override -}}
{{- $_414_key_ok := (get (fromJson (include "_shims.get" (dict "a" (list $el $mergeKey)))) "r") -}}
{{- $key := (index $_414_key_ok 0) -}}
{{- $ok := (index $_414_key_ok 1) -}}
{{- if (not $ok) -}}
{{- continue -}}
{{- end -}}
{{- $_ := (set $overrideByKey $key $el) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $merged := (coalesce nil) -}}
{{- range $_, $el := $original -}}
{{- $_426_key__ := (get (fromJson (include "_shims.get" (dict "a" (list $el $mergeKey)))) "r") -}}
{{- $key := (index $_426_key__ 0) -}}
{{- $_ := (index $_426_key__ 1) -}}
{{- $_ := (set $originalKeys $key true) -}}
{{- $_428_elOverride_5_ok_6 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $overrideByKey $key (coalesce nil))))) "r") -}}
{{- $elOverride_5 := (index $_428_elOverride_5_ok_6 0) -}}
{{- $ok_6 := (index $_428_elOverride_5_ok_6 1) -}}
{{- if $ok_6 -}}
{{- $merged = (concat (default (list) $merged) (list (get (fromJson (include (first $mergeFunc) (dict "a" (concat (rest $mergeFunc) (list $el $elOverride_5))))) "r"))) -}}
{{- else -}}
{{- $merged = (concat (default (list) $merged) (list $el)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $el := $override -}}
{{- $_438_key_ok := (get (fromJson (include "_shims.get" (dict "a" (list $el $mergeKey)))) "r") -}}
{{- $key := (index $_438_key_ok 0) -}}
{{- $ok := (index $_438_key_ok 1) -}}
{{- if (not $ok) -}}
{{- continue -}}
{{- end -}}
{{- $_443___ok_7 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $originalKeys $key false)))) "r") -}}
{{- $_ := (index $_443___ok_7 0) -}}
{{- $ok_7 := (index $_443___ok_7 1) -}}
{{- if $ok_7 -}}
{{- continue -}}
{{- end -}}
{{- $merged = (concat (default (list) $merged) (list (merge (dict) $el))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $merged) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.mergeEnvVar" -}}
{{- $original := (index .a 0) -}}
{{- $overrides := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $overrides)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.mergeVolume" -}}
{{- $original := (index .a 0) -}}
{{- $override := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $override)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.mergeVolumeMount" -}}
{{- $original := (index .a 0) -}}
{{- $override := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (merge (dict) $override $original)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.mergeContainer" -}}
{{- $original := (index .a 0) -}}
{{- $override := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $merged := (merge (dict) $override $original) -}}
{{- $_ := (set $merged "env" (get (fromJson (include "redpanda.mergeSliceBy" (dict "a" (list $original.env $override.env "name" (list "redpanda.mergeEnvVar"))))) "r")) -}}
{{- $_ := (set $merged "volumeMounts" (get (fromJson (include "redpanda.mergeSliceBy" (dict "a" (list $original.volumeMounts $override.volumeMounts "name" (list "redpanda.mergeVolumeMount"))))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $merged) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.PodNames" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $suffix := "" -}}
{{- if (ne $pool.Name "") -}}
{{- $suffix = (printf "-%s" $pool.Name) -}}
{{- end -}}
{{- $pods := (coalesce nil) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($pool.Statefulset.replicas | int)|int) (1|int) -}}
{{- $pods = (concat (default (list) $pods) (list (printf "%s%s-%d" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $suffix $i))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $pods) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

