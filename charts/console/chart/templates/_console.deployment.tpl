{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/console/v3/deployment.go" */ -}}

{{- define "console.ContainerPort" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $listenPort := ((8080 | int) | int) -}}
{{- if (ne (toJson $state.Values.service.targetPort) "null") -}}
{{- $listenPort = $state.Values.service.targetPort -}}
{{- end -}}
{{- $configListenPort := (dig "server" "listenPort" (coalesce nil) $state.Values.config) -}}
{{- $_34_asInt_1_ok_2 := (get (fromJson (include "_shims.asintegral" (dict "a" (list $configListenPort)))) "r") -}}
{{- $asInt_1 := ((index $_34_asInt_1_ok_2 0) | int) -}}
{{- $ok_2 := (index $_34_asInt_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $_is_returning = true -}}
{{- (dict "r" ($asInt_1 | int)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $listenPort) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.Deployment" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.deployment.create) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $replicas := (coalesce nil) -}}
{{- if (not $state.Values.autoscaling.enabled) -}}
{{- $replicas = ($state.Values.replicaCount | int) -}}
{{- end -}}
{{- $initContainers := (coalesce nil) -}}
{{- if (not (empty $state.Values.initContainers.extraInitContainers)) -}}
{{- $initContainers = (fromYamlArray (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $state.Values.initContainers.extraInitContainers))))) "r")) -}}
{{- end -}}
{{- $volumeMounts := (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "configs" "mountPath" "/etc/console/configs" "readOnly" true))) -}}
{{- if $state.Values.secret.create -}}
{{- $volumeMounts = (concat (default (list) $volumeMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" "secrets" "mountPath" "/etc/console/secrets" "readOnly" true)))) -}}
{{- end -}}
{{- range $_, $mount := $state.Values.secretMounts -}}
{{- $volumeMounts = (concat (default (list) $volumeMounts) (list (mustMergeOverwrite (dict "name" "" "mountPath" "") (dict "name" $mount.name "mountPath" $mount.path "subPath" (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $mount.subPath "")))) "r"))))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $volumeMounts = (concat (default (list) $volumeMounts) (default (list) $state.Values.extraVolumeMounts)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) "status" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "apps/v1" "kind" "Deployment")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r") "labels" (get (fromJson (include "console.RenderState.Labels" (dict "a" (list $state (coalesce nil))))) "r") "namespace" $state.Namespace "annotations" $state.Values.annotations)) "spec" (mustMergeOverwrite (dict "selector" (coalesce nil) "template" (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) "strategy" (dict)) (dict "replicas" $replicas "selector" (mustMergeOverwrite (dict) (dict "matchLabels" (get (fromJson (include "console.RenderState.SelectorLabels" (dict "a" (list $state)))) "r"))) "strategy" $state.Values.strategy "template" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil)) "spec" (dict "containers" (coalesce nil))) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "annotations" (merge (dict) (dict "checksum/config" (sha256sum (toYaml (get (fromJson (include "console.ConfigMap" (dict "a" (list $state)))) "r").data))) $state.Values.podAnnotations) "labels" (merge (dict) (get (fromJson (include "console.RenderState.SelectorLabels" (dict "a" (list $state)))) "r") $state.Values.podLabels))) "spec" (mustMergeOverwrite (dict "containers" (coalesce nil)) (dict "imagePullSecrets" $state.Values.imagePullSecrets "serviceAccountName" (get (fromJson (include "console.ServiceAccountName" (dict "a" (list $state)))) "r") "automountServiceAccountToken" $state.Values.automountServiceAccountToken "securityContext" $state.Values.podSecurityContext "nodeSelector" $state.Values.nodeSelector "affinity" $state.Values.affinity "topologySpreadConstraints" $state.Values.topologySpreadConstraints "priorityClassName" $state.Values.priorityClassName "tolerations" $state.Values.tolerations "volumes" (get (fromJson (include "console.consolePodVolumes" (dict "a" (list $state)))) "r") "initContainers" $initContainers "containers" (concat (default (list) (list (mustMergeOverwrite (dict "name" "" "resources" (dict)) (dict "name" "console" "command" $state.Values.deployment.command "args" (concat (default (list) (list "--config.filepath=/etc/console/configs/config.yaml")) (default (list) $state.Values.deployment.extraArgs)) "securityContext" $state.Values.securityContext "image" (get (fromJson (include "console.containerImage" (dict "a" (list $state)))) "r") "imagePullPolicy" $state.Values.image.pullPolicy "ports" (concat (default (list) (list (mustMergeOverwrite (dict "containerPort" 0) (dict "name" "http" "containerPort" ((get (fromJson (include "console.ContainerPort" (dict "a" (list $state)))) "r") | int) "protocol" "TCP")))) (default (list) $state.Values.extraContainerPorts)) "volumeMounts" $volumeMounts "livenessProbe" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "httpGet" (mustMergeOverwrite (dict "port" 0) (dict "path" "/admin/health" "port" "http")))) (dict "initialDelaySeconds" ($state.Values.livenessProbe.initialDelaySeconds | int) "periodSeconds" ($state.Values.livenessProbe.periodSeconds | int) "timeoutSeconds" ($state.Values.livenessProbe.timeoutSeconds | int) "successThreshold" ($state.Values.livenessProbe.successThreshold | int) "failureThreshold" ($state.Values.livenessProbe.failureThreshold | int))) "readinessProbe" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "httpGet" (mustMergeOverwrite (dict "port" 0) (dict "path" "/admin/health" "port" "http")))) (dict "initialDelaySeconds" ($state.Values.readinessProbe.initialDelaySeconds | int) "periodSeconds" ($state.Values.readinessProbe.periodSeconds | int) "timeoutSeconds" ($state.Values.readinessProbe.timeoutSeconds | int) "successThreshold" ($state.Values.readinessProbe.successThreshold | int) "failureThreshold" ($state.Values.readinessProbe.failureThreshold | int))) "resources" $state.Values.resources "env" (get (fromJson (include "console.consoleContainerEnv" (dict "a" (list $state)))) "r") "envFrom" $state.Values.extraEnvFrom)))) (default (list) $state.Values.extraContainers))))))))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.containerImage" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tag := $state.Values.image.tag -}}
{{- if (eq $tag "") -}}
{{- $tag = "v3.3.2" -}}
{{- end -}}
{{- $image := (printf "%s:%s" $state.Values.image.repository $tag) -}}
{{- if (not (empty $state.Values.image.registry)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s/%s" $state.Values.image.registry $image)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $image) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.consoleContainerEnv" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.secret.create) -}}
{{- $vars := $state.Values.extraEnv -}}
{{- if (and (ne (toJson $state.Values.licenseSecretRef) "null") (not (empty $state.Values.licenseSecretRef.name))) -}}
{{- $vars = (concat (default (list) $state.Values.extraEnv) (list (mustMergeOverwrite (dict "name" "") (dict "name" "LICENSE" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" $state.Values.licenseSecretRef.name)) (dict "key" (default "enterprise-license" $state.Values.licenseSecretRef.key))))))))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $vars) | toJson -}}
{{- break -}}
{{- end -}}
{{- $possibleVars := (list (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.kafka.saslPassword "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "KAFKA_SASL_PASSWORD" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "kafka-sasl-password")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.serde.protobufGitBasicAuthPassword "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SERDE_PROTOBUF_GIT_BASICAUTH_PASSWORD" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "serde-protobuf-git-basicauth-password")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.kafka.awsMskIamSecretKey "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "KAFKA_SASL_AWSMSKIAM_SECRETKEY" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "kafka-sasl-aws-msk-iam-secret-key")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.kafka.tlsCa "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "KAFKA_TLS_CAFILEPATH" "value" "/etc/console/secrets/kafka-tls-ca")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.kafka.tlsCert "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "KAFKA_TLS_CERTFILEPATH" "value" "/etc/console/secrets/kafka-tls-cert")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.kafka.tlsKey "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "KAFKA_TLS_KEYFILEPATH" "value" "/etc/console/secrets/kafka-tls-key")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.schemaRegistry.tlsCa "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SCHEMAREGISTRY_TLS_CAFILEPATH" "value" "/etc/console/secrets/schemaregistry-tls-ca")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.schemaRegistry.tlsCert "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SCHEMAREGISTRY_TLS_CERTFILEPATH" "value" "/etc/console/secrets/schemaregistry-tls-cert")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.schemaRegistry.tlsKey "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SCHEMAREGISTRY_TLS_KEYFILEPATH" "value" "/etc/console/secrets/schemaregistry-tls-key")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.schemaRegistry.password "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "schema-registry-password")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.schemaRegistry.bearerToken "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "schema-registry-bearertoken")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.authentication.jwtSigningKey "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "AUTHENTICATION_JWTSIGNINGKEY" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "authentication-jwt-signingkey")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.license "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "LICENSE" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "license")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.redpanda.adminApi.password "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_ADMINAPI_PASSWORD" "valueFrom" (mustMergeOverwrite (dict) (dict "secretKeyRef" (mustMergeOverwrite (dict "key" "") (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict "key" "redpanda-admin-api-password")))))))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.redpanda.adminApi.tlsCa "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_ADMINAPI_TLS_CAFILEPATH" "value" "/etc/console/secrets/redpanda-admin-api-tls-ca")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.redpanda.adminApi.tlsKey "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_ADMINAPI_TLS_KEYFILEPATH" "value" "/etc/console/secrets/redpanda-admin-api-tls-key")))) (mustMergeOverwrite (dict "Value" (coalesce nil) "EnvVar" (dict "name" "")) (dict "Value" $state.Values.secret.redpanda.adminApi.tlsCert "EnvVar" (mustMergeOverwrite (dict "name" "") (dict "name" "REDPANDA_ADMINAPI_TLS_CERTFILEPATH" "value" "/etc/console/secrets/redpanda-admin-api-tls-cert"))))) -}}
{{- $vars := $state.Values.extraEnv -}}
{{- range $_, $possible := $possibleVars -}}
{{- if (not (empty $possible.Value)) -}}
{{- $vars = (concat (default (list) $vars) (list $possible.EnvVar)) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $vars) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "console.consolePodVolumes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $volumes := (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "configMap" (mustMergeOverwrite (dict) (mustMergeOverwrite (dict) (dict "name" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))) (dict)))) (dict "name" "configs"))) -}}
{{- if $state.Values.secret.create -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" (get (fromJson (include "console.RenderState.FullName" (dict "a" (list $state)))) "r"))))) (dict "name" "secrets")))) -}}
{{- end -}}
{{- range $_, $mount := $state.Values.secretMounts -}}
{{- $volumes = (concat (default (list) $volumes) (list (mustMergeOverwrite (dict "name" "") (mustMergeOverwrite (dict) (dict "secret" (mustMergeOverwrite (dict) (dict "secretName" $mount.secretName "defaultMode" $mount.defaultMode)))) (dict "name" $mount.name)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $volumes) (default (list) $state.Values.extraVolumes))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

