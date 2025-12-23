{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/operator/chart/multicluster_deployment.go" */ -}}

{{- define "operator.MulticlusterDeployment" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- if (eq $values.multicluster.apiServerExternalAddress "") -}}
{{- $_ := (fail "apiServerExternalAddress must be specified in multicluster mode") -}}
{{- end -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $values.multicluster.peers)))) "r") | int) (0 | int)) -}}
{{- $_ := (fail "peers must be specified in multicluster mode") -}}
{{- end -}}
{{- $dep := (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "apps/v1" "kind" "Deployment")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r") "labels" (get (fromJson (include "operator.Labels" (dict "a" (list $dot)))) "r") "namespace" $dot.Release.Namespace "annotations" $values.annotations)) "spec" (mustMergeOverwrite (dict "selector" (coalesce nil) "template" (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) (dict "replicas" ($values.replicaCount | int) "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "operator.SelectorLabels" (dict "a" (list $dot)))) "r"))) "strategy" $values.strategy "template" (get (fromJson (include "operator.StrategicMergePatch" (dict "a" (list (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "labels" $values.podTemplate.metadata.labels "annotations" $values.podTemplate.metadata.annotations)) "spec" $values.podTemplate.spec)) (mustMergeOverwrite (dict "metadata" (dict) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict) (dict "annotations" $values.podAnnotations "labels" (merge (dict) (get (fromJson (include "operator.SelectorLabels" (dict "a" (list $dot)))) "r") $values.podLabels))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "automountServiceAccountToken" false "terminationGracePeriodSeconds" ((10 | int64) | int64) "imagePullSecrets" $values.imagePullSecrets "serviceAccountName" (get (fromJson (include "operator.ServiceAccountName" (dict "a" (list $dot)))) "r") "nodeSelector" $values.nodeSelector "tolerations" $values.tolerations "volumes" (get (fromJson (include "operator.multiclusterOperatorPodVolumes" (dict "a" (list $dot)))) "r") "containers" (get (fromJson (include "operator.multiclusterOperatorContainers" (dict "a" (list $dot (coalesce nil))))) "r"))))))))) "r"))))) -}}
{{- if (not (empty $values.affinity)) -}}
{{- $_ := (set $dep.spec.template.spec "affinity" $values.affinity) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $dep) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.multiclusterOperatorContainers" -}}
{{- $dot := (index .a 0) -}}
{{- $podTerminationGracePeriodSeconds := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "manager" "image" (get (fromJson (include "operator.containerImage" (dict "a" (list $dot)))) "r") "imagePullPolicy" $values.image.pullPolicy "command" (list "/manager") "args" (get (fromJson (include "operator.multiclusterOperatorArguments" (dict "a" (list $dot)))) "r") "securityContext" (mustMergeOverwrite (dict) (dict "allowPrivilegeEscalation" false)) "ports" (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "webhook-server" "containerPort" (9443 | int) "protocol" "TCP")) (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "https" "containerPort" (8443 | int) "protocol" "TCP"))) "volumeMounts" (get (fromJson (include "operator.multiclusterOperatorPodVolumesMounts" (dict "a" (list $dot)))) "r") "livenessProbe" (get (fromJson (include "operator.livenessProbe" (dict "a" (list $dot $podTerminationGracePeriodSeconds)))) "r") "readinessProbe" (get (fromJson (include "operator.readinessProbe" (dict "a" (list $dot $podTerminationGracePeriodSeconds)))) "r") "resources" $values.resources)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.multiclusterOperatorPodVolumes" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $vol := (list (get (fromJson (include "operator.serviceAccountTokenVolume" (dict "a" (list)))) "r") (get (fromJson (include "operator.multiclusterTLSVolume" (dict "a" (list $dot)))) "r")) -}}
{{- if (not $values.webhook.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $vol) | toJson -}}
{{- break -}}
{{- end -}}
{{- $vol = (concat (default (list) $vol) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "defaultMode" ((420 | int) | int) "secretName" $values.webhookSecretName)))) (dict "name" "cert")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $vol) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.multiclusterOperatorPodVolumesMounts" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $volMount := (list (get (fromJson (include "operator.serviceAccountTokenVolumeMount" (dict "a" (list)))) "r") (get (fromJson (include "operator.multiclusterTLSVolumeMount" (dict "a" (list $dot)))) "r")) -}}
{{- if (not $values.webhook.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $volMount) | toJson -}}
{{- break -}}
{{- end -}}
{{- $volMount = (concat (default (list) $volMount) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "cert" "mountPath" "/tmp/k8s-webhook-server/serving-certs" "readOnly" true)))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $volMount) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.multiclusterOperatorArguments" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $values := $dot.Values.AsMap -}}
{{- $defaults := (dict "--health-probe-bind-address" ":8081" "--metrics-bind-address" ":8443" "--log-level" $values.logLevel "--base-tag" (get (fromJson (include "operator.containerTag" (dict "a" (list $dot)))) "r") "--base-image" $values.image.repository "--raft-address" "0.0.0.0:9443" "--ca-file" "/tls/ca.crt" "--certificate-file" "/tls/tls.crt" "--private-key-file" "/tls/tls.key" "--kubernetes-api-address" $values.multicluster.apiServerExternalAddress "--kubeconfig-namespace" $dot.Release.Namespace "--kubeconfig-name" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r")) -}}
{{- if $values.webhook.enabled -}}
{{- $_ := (set $defaults "--webhook-cert-path" (printf "%s%s" "/tmp/k8s-webhook-server/serving-certs" "/tls.crt")) -}}
{{- $_ := (set $defaults "--webhook-key-path" (printf "%s%s" "/tmp/k8s-webhook-server/serving-certs" "/tls.key")) -}}
{{- end -}}
{{- $userProvided := (get (fromJson (include "chartutil.ParseFlags" (dict "a" (list $values.additionalCmdFlags)))) "r") -}}
{{- $flags := (list "multicluster") -}}
{{- range $key, $value := (merge (dict) $defaults $userProvided) -}}
{{- if (eq $value "") -}}
{{- $flags = (concat (default (list) $flags) (list $key)) -}}
{{- else -}}
{{- $flags = (concat (default (list) $flags) (list (printf "%s=%s" $key $value))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $peer := $values.multicluster.peers -}}
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

{{- define "operator.multiclusterTLSVolume" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (printf "%s-multicluster-certificates" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r")) "items" (list (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "tls.crt" "path" "tls.crt")) (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "tls.key" "path" "tls.key")) (mustMergeOverwrite (dict "key" "" "path" "") (dict "key" "ca.crt" "path" "ca.crt"))))))) (dict "name" (printf "%s-multicluster-certificates" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "operator.multiclusterTLSVolumeMount" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" (printf "%s-multicluster-certificates" (get (fromJson (include "operator.Fullname" (dict "a" (list $dot)))) "r")) "readOnly" true "mountPath" "/tls"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

