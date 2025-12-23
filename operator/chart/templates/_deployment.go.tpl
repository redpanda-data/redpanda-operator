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
{{- $vol := (list (get (fromJson (include "operator.serviceAccountTokenVolume" (dict "a" (list)))) "r")) -}}
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
{{- $volMount := (list (get (fromJson (include "operator.serviceAccountTokenVolumeMount" (dict "a" (list)))) "r")) -}}
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
{{- $defaults := (dict "--health-probe-bind-address" ":8081" "--metrics-bind-address" ":8443" "--leader-elect" "" "--enable-console" "true" "--log-level" $values.logLevel "--webhook-enabled" (printf "%t" $values.webhook.enabled) "--configurator-tag" (get (fromJson (include "operator.containerTag" (dict "a" (list $dot)))) "r") "--configurator-base-image" $values.image.repository "--enable-vectorized-controllers" (printf "%t" $values.vectorizedControllers.enabled)) -}}
{{- if $values.webhook.enabled -}}
{{- $_ := (set $defaults "--webhook-cert-path" "/tmp/k8s-webhook-server/serving-certs") -}}
{{- end -}}
{{- $userProvided := (get (fromJson (include "chartutil.ParseFlags" (dict "a" (list $values.additionalCmdFlags)))) "r") -}}
{{- $flags := (coalesce nil) -}}
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
{{- $_is_returning = true -}}
{{- (dict "r" $flags) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

