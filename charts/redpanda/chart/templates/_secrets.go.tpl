{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/secrets.go" */ -}}

{{- define "redpanda.Secrets" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $secrets := (coalesce nil) -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretSTSLifecycle" (dict "a" (list $state)))) "r"))) -}}
{{- $saslUsers_1 := (get (fromJson (include "redpanda.SecretSASLUsers" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $saslUsers_1) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $saslUsers_1)) -}}
{{- end -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretConfigurator" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)) (0 | int))))) "r"))) -}}
{{- $fsValidator_2 := (get (fromJson (include "redpanda.SecretFSValidator" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- if (ne (toJson $fsValidator_2) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $fsValidator_2)) -}}
{{- end -}}
{{- $ordinalOffset := (($state.Values.statefulset.replicas | int) | int) -}}
{{- range $_, $set := $state.Pools -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretConfigurator" (dict "a" (list $state $set $ordinalOffset)))) "r"))) -}}
{{- $fsValidator_3 := (get (fromJson (include "redpanda.SecretFSValidator" (dict "a" (list $state $set)))) "r") -}}
{{- if (ne (toJson $fsValidator_3) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $fsValidator_3)) -}}
{{- end -}}
{{- $ordinalOffset = ((add $ordinalOffset (($set.Statefulset.replicas | int) | int)) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $bootstrapUser_4 := (get (fromJson (include "redpanda.SecretBootstrapUser" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $bootstrapUser_4) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $bootstrapUser_4)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secrets) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretSTSLifecycle" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $replicas := ($state.Values.statefulset.replicas | int) -}}
{{- range $_, $set := $state.Pools -}}
{{- $replicas = ((add $replicas ($set.Statefulset.replicas | int)) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $adminCurlFlags := (get (fromJson (include "redpanda.adminTLSCurlFlags" (dict "a" (list $state)))) "r") -}}
{{- $drain := (and (gt $replicas (2 | int)) (not (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "recovery_mode_enabled" false $state.Values.config.node))))) "r"))) -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-sts-lifecycle" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict "common.sh" (get (fromJson (include "_redpanda.LifecycleCommonSh" (dict "a" (list (get (fromJson (include "redpanda.adminInternalURL" (dict "a" (list $state)))) "r") $adminCurlFlags (printf "${SERVICE_NAME}.%s" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")))))) "r") "postStart.sh" (get (fromJson (include "_redpanda.LifecyclePostStartSh" (dict "a" (list $adminCurlFlags)))) "r") "preStop.sh" (get (fromJson (include "_redpanda.LifecyclePreStopSh" (dict "a" (list $adminCurlFlags $drain)))) "r")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretSASLUsers" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (and (ne $state.Values.auth.sasl.secretRef "") $state.Values.auth.sasl.enabled) (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.auth.sasl.users)))) "r") | int) (0 | int))) -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $state.Values.auth.sasl.secretRef "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict))) -}}
{{- $usersTxt := (list) -}}
{{- $defaultMechanism := (get (fromJson (include "redpanda.SASLAuth.GetMechanism" (dict "a" (list $state.Values.auth.sasl)))) "r") -}}
{{- range $_, $user := $state.Values.auth.sasl.users -}}
{{- $mechanism := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $user.mechanism $defaultMechanism)))) "r") -}}
{{- $usersTxt = (concat (default (list) $usersTxt) (list (printf "%s:%s:%s" $user.name $user.password $mechanism))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $secret.stringData "users.txt" (join "\n" $usersTxt)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- else -}}{{- if (and $state.Values.auth.sasl.enabled (eq $state.Values.auth.sasl.secretRef "")) -}}
{{- $_ := (fail "auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true") -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretBootstrapUser" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.auth.sasl.enabled) (ne (toJson $state.Values.auth.sasl.bootstrapUser.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secretName := (printf "%s-bootstrap-user" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) -}}
{{- if (ne (toJson $state.BootstrapUserSecret) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.BootstrapUserSecret) | toJson -}}
{{- break -}}
{{- end -}}
{{- $password := (randAlphaNum (32 | int)) -}}
{{- $userPassword := $state.Values.auth.sasl.bootstrapUser.password -}}
{{- if (ne (toJson $userPassword) "null") -}}
{{- $password = $userPassword -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $secretName "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "immutable" true "type" "Opaque" "stringData" (dict "password" $password)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretFSValidator" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $pool.Statefulset.initContainers.fsValidator.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%.49s-fs-validator" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r"))) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict))) -}}
{{- $_ := (set $secret.stringData "fsValidator.sh" (get (fromJson (include "_redpanda.FSValidatorSh" (dict "a" (list)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretConfigurator" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $configuratorSh := (get (fromJson (include "_redpanda.ConfiguratorPrologueSh" (dict "a" (list)))) "r") -}}
{{- $kafkaSnippet := (get (fromJson (include "redpanda.secretConfiguratorKafkaConfig" (dict "a" (list $state $pool.Statefulset $ordinalOffset)))) "r") -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) $kafkaSnippet)) -}}
{{- $httpSnippet := (get (fromJson (include "redpanda.secretConfiguratorHTTPConfig" (dict "a" (list $state $pool.Statefulset $ordinalOffset)))) "r") -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) $httpSnippet)) -}}
{{- if $state.Values.rackAwareness.enabled -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) (get (fromJson (include "_redpanda.ConfiguratorRackAwarenessSh" (dict "a" (list $state.Values.rackAwareness.nodeAnnotation)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%.51s-configurator" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r"))) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict "configurator.sh" (join "\n" $configuratorSh))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.secretConfiguratorKafkaConfig" -}}
{{- $state := (index .a 0) -}}
{{- $sts := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $internalAdvertiseAddress := (printf "%s.%s" "${SERVICE_NAME}" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) -}}
{{- $snippet := (coalesce nil) -}}
{{- $listenerName := "kafka" -}}
{{- $listenerAdvertisedName := $listenerName -}}
{{- $redpandaConfigPart := "redpanda" -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `LISTENER=%s` (quote (toJson (dict "name" "internal" "address" $internalAdvertiseAddress "port" ($state.Values.listeners.kafka.port | int))))) (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"` $redpandaConfigPart $listenerAdvertisedName))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.listeners.kafka.external)))) "r") | int) (0 | int)) -}}
{{- $externalCounter := (0 | int) -}}
{{- range $externalName, $externalVals := $state.Values.listeners.kafka.external -}}
{{- $externalCounter = ((add $externalCounter (1 | int)) | int) -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `ADVERTISED_%s_ADDRESSES=()` (upper $listenerName)))) -}}
{{- range $_, $replicaIndex := (until (($sts.replicas | int) | int)) -}}
{{- $port := ($externalVals.port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $externalVals.advertisedPorts (0 | int)) -}}
{{- else -}}
{{- $port = (index $externalVals.advertisedPorts $replicaIndex) -}}
{{- end -}}
{{- end -}}
{{- $host := (get (fromJson (include "redpanda.advertisedHostJSON" (dict "a" (list $state $externalName $port $replicaIndex ((add $ordinalOffset $replicaIndex) | int) (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.hostTemplate "")))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $externalVals)))) "r"))))) "r") -}}
{{- $address := (toJson $host) -}}
{{- $prefixTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.prefixTemplate "")))) "r") -}}
{{- if (eq $prefixTemplate "") -}}
{{- $prefixTemplate = (default "" $state.Values.external.prefixTemplate) -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `PREFIX_TEMPLATE=%s` (quote $prefixTemplate)) (printf `ADVERTISED_%s_ADDRESSES+=(%s)` (upper $listenerName) (quote $address)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"` $redpandaConfigPart $listenerAdvertisedName $externalCounter (upper $listenerName)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $snippet) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.secretConfiguratorHTTPConfig" -}}
{{- $state := (index .a 0) -}}
{{- $sts := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $internalAdvertiseAddress := (printf "%s.%s" "${SERVICE_NAME}" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) -}}
{{- $snippet := (coalesce nil) -}}
{{- $listenerName := "http" -}}
{{- $listenerAdvertisedName := "pandaproxy" -}}
{{- $redpandaConfigPart := "pandaproxy" -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `LISTENER=%s` (quote (toJson (dict "name" "internal" "address" $internalAdvertiseAddress "port" ($state.Values.listeners.http.port | int))))) (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"` $redpandaConfigPart $listenerAdvertisedName))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.listeners.http.external)))) "r") | int) (0 | int)) -}}
{{- $externalCounter := (0 | int) -}}
{{- range $externalName, $externalVals := $state.Values.listeners.http.external -}}
{{- $externalCounter = ((add $externalCounter (1 | int)) | int) -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `ADVERTISED_%s_ADDRESSES=()` (upper $listenerName)))) -}}
{{- range $_, $replicaIndex := (until (($sts.replicas | int) | int)) -}}
{{- $port := ($externalVals.port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $externalVals.advertisedPorts (0 | int)) -}}
{{- else -}}
{{- $port = (index $externalVals.advertisedPorts $replicaIndex) -}}
{{- end -}}
{{- end -}}
{{- $host := (get (fromJson (include "redpanda.advertisedHostJSON" (dict "a" (list $state $externalName $port $replicaIndex ((add $ordinalOffset $replicaIndex) | int) (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.hostTemplate "")))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $externalVals)))) "r"))))) "r") -}}
{{- $address := (toJson $host) -}}
{{- $prefixTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.prefixTemplate "")))) "r") -}}
{{- if (eq $prefixTemplate "") -}}
{{- $prefixTemplate = (default "" $state.Values.external.prefixTemplate) -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `PREFIX_TEMPLATE=%s` (quote $prefixTemplate)) (printf `ADVERTISED_%s_ADDRESSES+=(%s)` (upper $listenerName) (quote $address)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"` $redpandaConfigPart $listenerAdvertisedName $externalCounter (upper $listenerName)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $snippet) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminTLSCurlFlags" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- if $state.Values.listeners.admin.tls.requireClientAuth -}}
{{- $path := (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key" $path $path $path)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $path := (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s" $path)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.externalAdvertiseAddress" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $eaa := "${SERVICE_NAME}" -}}
{{- $externalDomainTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") -}}
{{- $expanded := (tpl $externalDomainTemplate $state.Dot) -}}
{{- if (not (empty $expanded)) -}}
{{- $eaa = (printf "%s.%s" "${SERVICE_NAME}" $expanded) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $eaa) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedHostJSON" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $port := (index .a 2) -}}
{{- $replicaIndex := (index .a 3) -}}
{{- $globalOrdinal := (index .a 4) -}}
{{- $host := (index .a 5) -}}
{{- $hostTemplate := (index .a 6) -}}
{{- $isGateway := (index .a 7) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") $isGateway) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.advertisedHostJSONGateway" (dict "a" (list $state $name $globalOrdinal $host $hostTemplate)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $hostMap := (dict "name" $name "address" (get (fromJson (include "redpanda.externalAdvertiseAddress" (dict "a" (list $state)))) "r") "port" $port) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (0 | int)) -}}
{{- $address := "" -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (1 | int)) -}}
{{- $address = (index $state.Values.external.addresses $replicaIndex) -}}
{{- else -}}
{{- $address = (index $state.Values.external.addresses (0 | int)) -}}
{{- end -}}
{{- $domain_5 := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") -}}
{{- if (ne $domain_5 "") -}}
{{- $hostMap = (dict "name" $name "address" (printf "%s.%s" $address (tpl $domain_5 $state.Dot)) "port" $port) -}}
{{- else -}}
{{- $hostMap = (dict "name" $name "address" $address "port" $port) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $hostMap) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedHostJSONGateway" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $globalOrdinal := (index .a 2) -}}
{{- $host := (index .a 3) -}}
{{- $hostTemplate := (index .a 4) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $gw := $state.Values.external.gateway -}}
{{- $port := ((get (fromJson (include "redpanda.GatewayConfig.GatewayAdvertisedPort" (dict "a" (list $gw)))) "r") | int) -}}
{{- if (eq $hostTemplate "") -}}
{{- $hostTemplate = $host -}}
{{- end -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $podName := "" -}}
{{- if (lt $globalOrdinal ((get (fromJson (include "_shims.len" (dict "a" (list $pods)))) "r") | int)) -}}
{{- $podName = (index $pods $globalOrdinal) -}}
{{- end -}}
{{- $address := (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $globalOrdinal $podName)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "name" $name "address" $address "port" $port)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminInternalHTTPProtocol" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "https") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "http") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminInternalURL" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s://%s.%s:%d" (get (fromJson (include "redpanda.adminInternalHTTPProtocol" (dict "a" (list $state)))) "r") `${SERVICE_NAME}` (trimSuffix "." (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) ($state.Values.listeners.admin.port | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

