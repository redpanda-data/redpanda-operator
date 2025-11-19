{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/multicluster/v25/deployment.go" */ -}}

{{- define "multicluster.Deployment" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $dep := (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "apps/v1" "kind" "Deployment")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r") "labels" (get (fromJson (include "multicluster.Labels" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace)) "spec" (mustMergeOverwrite (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) (dict "replicas" ((1 | int) | int) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "multicluster.SelectorLabels" (dict "a" (list $dot)))) "r"))) "template" (get (fromJson (include "multicluster.StrategicMergePatch" (dict "a" (list (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" $values.podTemplate.metadata.labels "annotations" $values.podTemplate.metadata.annotations)) "spec" $values.podTemplate.spec)) (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "labels" (get (fromJson (include "multicluster.SelectorLabels" (dict "a" (list $dot)))) "r"))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "serviceAccountName" (get (fromJson (include "multicluster.ServiceAccountName" (dict "a" (list $dot)))) "r") "volumes" (get (fromJson (include "multicluster.operatorPodVolumes" (dict "a" (list $dot)))) "r") "containers" (get (fromJson (include "multicluster.operatorContainers" (dict "a" (list $dot)))) "r"))))))))) "r"))))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $dep) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.operatorPodVolumes" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "multicluster.tlsVolume" (dict "a" (list $dot)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.operatorContainers" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "manager" "image" (get (fromJson (include "multicluster.containerImage" (dict "a" (list $dot)))) "r") "command" (list "/manager") "args" (concat (default (list) (list "multicluster")) (default (list) (get (fromJson (include "multicluster.operatorArguments" (dict "a" (list $dot)))) "r"))) "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false)) "ports" (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "https" "containerPort" (9443 | int) "protocol" "TCP"))) "volumeMounts" (get (fromJson (include "multicluster.operatorPodVolumesMounts" (dict "a" (list $dot)))) "r") "resources" $values.resources)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.operatorPodVolumesMounts" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (get (fromJson (include "multicluster.tlsVolumeMount" (dict "a" (list $dot)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.tlsVolumeMount" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf "%s-certificates" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r")) "readOnly" true "mountPath" "/tls"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.tlsVolume" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (printf "%s-certificates" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r")) "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "tls.crt" "path" "tls.crt")) (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "tls.key" "path" "tls.key")) (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "ca.crt" "path" "ca.crt"))))))) (dict "name" (printf "%s-certificates" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.operatorArguments" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $defaults := (dict "--name" $values.node "--address" "0.0.0.0:9443" "--ca-file" "/tls/ca.crt" "--certificate-file" "/tls/tls.crt" "--private-key-file" "/tls/tls.key" "--kubernetes-api-address" $values.apiServerExternalAddress "--kubeconfig-namespace" $dot.Release.Namespace "--kubeconfig-name" (get (fromJson (include "multicluster.Fullname" (dict "a" (list $dot)))) "r") "--log-level" $values.logLevel) -}}
{{- $flags := (coalesce nil) -}}
{{- range $key, $value := $defaults -}}
{{- if (eq $value "") -}}
{{- $flags = (concat (default (list) $flags) (list $key)) -}}
{{- else -}}
{{- $flags = (concat (default (list) $flags) (list (printf "%s=%s" $key $value))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $peer := $values.peers -}}
{{- $flags = (concat (default (list) $flags) (list (printf "--peer=%s://%s:9443" $peer.name $peer.address))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $flags) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.containerTag" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (not (empty $values.image.tag)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $values.image.tag) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $dot.Chart.AppVersion) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "multicluster.containerImage" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $tag := (get (fromJson (include "multicluster.containerTag" (dict "a" (list $dot)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s:%s" $values.image.repository $tag)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

