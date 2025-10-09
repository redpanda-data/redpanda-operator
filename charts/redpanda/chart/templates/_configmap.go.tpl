{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/configmap.tpl.go" */ -}}

{{- define "redpanda.ConfigMaps" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $cms := (list (get (fromJson (include "redpanda.RedpandaConfigMap" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" ""))) (dict "Statefulset" $state.Values.statefulset)))))) "r")) -}}
{{- range $_, $set := $state.Pools -}}
{{- $cms = (concat (default (list) $cms) (list (get (fromJson (include "redpanda.RedpandaConfigMap" (dict "a" (list $state $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $cms) (list (get (fromJson (include "redpanda.RPKProfile" (dict "a" (list $state)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaConfigMap" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_36_bootstrap_fixups := (get (fromJson (include "redpanda.BootstrapFile" (dict "a" (list $state $pool)))) "r") -}}
{{- $bootstrap := (index $_36_bootstrap_fixups 0) -}}
{{- $fixups := (index $_36_bootstrap_fixups 1) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "ConfigMap" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "data" (dict ".bootstrap.json.in" $bootstrap "bootstrap.yaml.fixups" $fixups "redpanda.yaml" (get (fromJson (include "redpanda.RedpandaConfigFile" (dict "a" (list $state true $pool)))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.BootstrapFile" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_67_template_fixups := (get (fromJson (include "redpanda.BootstrapContents" (dict "a" (list $state $pool)))) "r") -}}
{{- $template := (index $_67_template_fixups 0) -}}
{{- $fixups := (index $_67_template_fixups 1) -}}
{{- $fixupStr := (toJson $fixups) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $fixups)))) "r") | int) (0 | int)) -}}
{{- $fixupStr = `[]` -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list (toJson $template) $fixupStr)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.BootstrapContents" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $fixups := (list) -}}
{{- $bootstrap := (dict "kafka_enable_authorization" (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") "enable_sasl" (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") "enable_rack_awareness" $state.Values.rackAwareness.enabled "storage_min_free_bytes" ((get (fromJson (include "redpanda.Storage.StorageMinFreeBytes" (dict "a" (list $state.Values.storage)))) "r") | int64)) -}}
{{- $bootstrap = (merge (dict) $bootstrap (get (fromJson (include "redpanda.AuditLogging.Translate" (dict "a" (list $state.Values.auditLogging $state (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r"))))) "r")) -}}
{{- $bootstrap = (merge (dict) $bootstrap (get (fromJson (include "redpanda.Logging.Translate" (dict "a" (list $state.Values.logging)))) "r")) -}}
{{- $bootstrap = (merge (dict) $bootstrap (get (fromJson (include "redpanda.TunableConfig.Translate" (dict "a" (list $state.Values.config.tunable)))) "r")) -}}
{{- $bootstrap = (merge (dict) $bootstrap (get (fromJson (include "redpanda.ClusterConfig.Translate" (dict "a" (list $state.Values.config.cluster)))) "r")) -}}
{{- $bootstrap = (merge (dict) $bootstrap (get (fromJson (include "redpanda.Auth.Translate" (dict "a" (list $state.Values.auth (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r"))))) "r")) -}}
{{- $_91_attrs_fixes := (get (fromJson (include "redpanda.TieredStorageConfig.Translate" (dict "a" (list (deepCopy (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r")) $state.Values.storage.tiered.credentialsSecretRef)))) "r") -}}
{{- $attrs := (index $_91_attrs_fixes 0) -}}
{{- $fixes := (index $_91_attrs_fixes 1) -}}
{{- $bootstrap = (merge (dict) $bootstrap $attrs) -}}
{{- $fixups = (concat (default (list) $fixups) (default (list) $fixes)) -}}
{{- $_101___ok_1 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $state.Values.config.cluster "default_topic_replications" (coalesce nil))))) "r") -}}
{{- $_ := (index $_101___ok_1 0) -}}
{{- $ok_1 := (index $_101___ok_1 1) -}}
{{- if (and (not $ok_1) (ge ($pool.Statefulset.replicas | int) (3 | int))) -}}
{{- $_ := (set $bootstrap "default_topic_replications" (3 | int)) -}}
{{- end -}}
{{- $_106___ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $state.Values.config.cluster "storage_min_free_bytes" (coalesce nil))))) "r") -}}
{{- $_ := (index $_106___ok_2 0) -}}
{{- $ok_2 := (index $_106___ok_2 1) -}}
{{- if (not $ok_2) -}}
{{- $_ := (set $bootstrap "storage_min_free_bytes" ((get (fromJson (include "redpanda.Storage.StorageMinFreeBytes" (dict "a" (list $state.Values.storage)))) "r") | int64)) -}}
{{- end -}}
{{- $template := (dict) -}}
{{- range $k, $v := $bootstrap -}}
{{- $_ := (set $template $k (toJson $v)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_117_extra_fixes__ := (get (fromJson (include "redpanda.ClusterConfiguration.Translate" (dict "a" (list (deepCopy $state.Values.config.extraClusterConfiguration))))) "r") -}}
{{- $extra := (index $_117_extra_fixes__ 0) -}}
{{- $fixes := (index $_117_extra_fixes__ 1) -}}
{{- $_ := (index $_117_extra_fixes__ 2) -}}
{{- $template = (merge (dict) $template $extra) -}}
{{- $fixups = (concat (default (list) $fixups) (default (list) $fixes)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $template $fixups)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaConfigFile" -}}
{{- $state := (index .a 0) -}}
{{- $includeNonHashableItems := (index .a 1) -}}
{{- $pool := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $redpanda := (dict "empty_seed_starts_cluster" false) -}}
{{- if $includeNonHashableItems -}}
{{- $servers := (get (fromJson (include "redpanda.Listeners.CreateSeedServers" (dict "a" (list $state.Values.listeners ($state.Values.statefulset.replicas | int) (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r"))))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $servers = (concat (default (list) $servers) (default (list) (get (fromJson (include "redpanda.Listeners.CreateSeedServers" (dict "a" (list $state.Values.listeners ($set.Statefulset.replicas | int) (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $set))))) "r")) (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r"))))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $redpanda "seed_servers" $servers) -}}
{{- end -}}
{{- $redpanda = (merge (dict) $redpanda (get (fromJson (include "redpanda.NodeConfig.Translate" (dict "a" (list $state.Values.config.node)))) "r")) -}}
{{- $_ := (get (fromJson (include "redpanda.configureListeners" (dict "a" (list $redpanda $state)))) "r") -}}
{{- $redpandaYaml := (dict "redpanda" $redpanda "schema_registry" (get (fromJson (include "redpanda.schemaRegistry" (dict "a" (list $state)))) "r") "pandaproxy" (get (fromJson (include "redpanda.pandaProxyListener" (dict "a" (list $state)))) "r") "config_file" "/etc/redpanda/redpanda.yaml") -}}
{{- if $includeNonHashableItems -}}
{{- $_ := (set $redpandaYaml "rpk" (get (fromJson (include "redpanda.rpkNodeConfig" (dict "a" (list $state $pool)))) "r")) -}}
{{- $_ := (set $redpandaYaml "pandaproxy_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $redpandaYaml "schema_registry_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state)))) "r")) -}}
{{- if (and (and (get (fromJson (include "redpanda.RedpandaAtLeast_23_3_0" (dict "a" (list $state)))) "r") $state.Values.auditLogging.enabled) (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r")) -}}
{{- $_ := (set $redpandaYaml "audit_log_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state)))) "r")) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (toYaml $redpandaYaml)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RPKProfile" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.external.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict "creationTimestamp" (coalesce nil))) (mustMergeOverwrite (dict) (dict "kind" "ConfigMap" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict "creationTimestamp" (coalesce nil)) (dict "name" (printf "%s-rpk" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "data" (dict "profile" (toYaml (get (fromJson (include "redpanda.rpkProfile" (dict "a" (list $state)))) "r")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkProfile" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokerList := (list) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($state.Values.statefulset.replicas | int)|int) (1|int) -}}
{{- $brokerList = (concat (default (list) $brokerList) (list (printf "%s:%d" (get (fromJson (include "redpanda.advertisedHost" (dict "a" (list $state $i)))) "r") (((get (fromJson (include "redpanda.advertisedKafkaPort" (dict "a" (list $state $i)))) "r") | int) | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $adminAdvertisedList := (list) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($state.Values.statefulset.replicas | int)|int) (1|int) -}}
{{- $adminAdvertisedList = (concat (default (list) $adminAdvertisedList) (list (printf "%s:%d" (get (fromJson (include "redpanda.advertisedHost" (dict "a" (list $state $i)))) "r") (((get (fromJson (include "redpanda.advertisedAdminPort" (dict "a" (list $state $i)))) "r") | int) | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $schemaAdvertisedList := (list) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($state.Values.statefulset.replicas | int)|int) (1|int) -}}
{{- $schemaAdvertisedList = (concat (default (list) $schemaAdvertisedList) (list (printf "%s:%d" (get (fromJson (include "redpanda.advertisedHost" (dict "a" (list $state $i)))) "r") (((get (fromJson (include "redpanda.advertisedSchemaPort" (dict "a" (list $state $i)))) "r") | int) | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $kafkaTLS := (get (fromJson (include "redpanda.rpkKafkaClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- $_206___ok_3 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $kafkaTLS "ca_file" (coalesce nil))))) "r") -}}
{{- $_ := (index $_206___ok_3 0) -}}
{{- $ok_3 := (index $_206___ok_3 1) -}}
{{- if $ok_3 -}}
{{- $_ := (set $kafkaTLS "ca_file" "ca.crt") -}}
{{- end -}}
{{- $adminTLS := (get (fromJson (include "redpanda.rpkAdminAPIClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- $_212___ok_4 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $adminTLS "ca_file" (coalesce nil))))) "r") -}}
{{- $_ := (index $_212___ok_4 0) -}}
{{- $ok_4 := (index $_212___ok_4 1) -}}
{{- if $ok_4 -}}
{{- $_ := (set $adminTLS "ca_file" "ca.crt") -}}
{{- end -}}
{{- $schemaTLS := (get (fromJson (include "redpanda.rpkSchemaRegistryClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- $_218___ok_5 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $schemaTLS "ca_file" (coalesce nil))))) "r") -}}
{{- $_ := (index $_218___ok_5 0) -}}
{{- $ok_5 := (index $_218___ok_5 1) -}}
{{- if $ok_5 -}}
{{- $_ := (set $schemaTLS "ca_file" "ca.crt") -}}
{{- end -}}
{{- $ka := (dict "brokers" $brokerList "tls" (coalesce nil)) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $kafkaTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $ka "tls" $kafkaTLS) -}}
{{- end -}}
{{- $aa := (dict "addresses" $adminAdvertisedList "tls" (coalesce nil)) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $adminTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $aa "tls" $adminTLS) -}}
{{- end -}}
{{- $sa := (dict "addresses" $schemaAdvertisedList "tls" (coalesce nil)) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $schemaTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $sa "tls" $schemaTLS) -}}
{{- end -}}
{{- $result := (dict "name" (get (fromJson (include "redpanda.getFirstExternalKafkaListener" (dict "a" (list $state)))) "r") "kafka_api" $ka "admin_api" $aa "schema_registry" $sa) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedKafkaPort" -}}
{{- $state := (index .a 0) -}}
{{- $i := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $externalKafkaListenerName := (get (fromJson (include "redpanda.getFirstExternalKafkaListener" (dict "a" (list $state)))) "r") -}}
{{- $listener := (ternary (index $state.Values.listeners.kafka.external $externalKafkaListenerName) (dict "enabled" (coalesce nil) "advertisedPorts" (coalesce nil) "port" 0 "nodePort" (coalesce nil) "tls" (coalesce nil)) (hasKey $state.Values.listeners.kafka.external $externalKafkaListenerName)) -}}
{{- $port := (($state.Values.listeners.kafka.port | int) | int) -}}
{{- if (gt (($listener.port | int) | int) ((1 | int) | int)) -}}
{{- $port = (($listener.port | int) | int) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts $i) | int) -}}
{{- else -}}{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts (0 | int)) | int) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $port) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedAdminPort" -}}
{{- $state := (index .a 0) -}}
{{- $i := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $keys := (keys $state.Values.listeners.admin.external) -}}
{{- $_ := (sortAlpha $keys) -}}
{{- $externalAdminListenerName := (first $keys) -}}
{{- $listener := (ternary (index $state.Values.listeners.admin.external (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $externalAdminListenerName)))) "r")) (dict "enabled" (coalesce nil) "advertisedPorts" (coalesce nil) "port" 0 "nodePort" (coalesce nil) "tls" (coalesce nil)) (hasKey $state.Values.listeners.admin.external (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $externalAdminListenerName)))) "r"))) -}}
{{- $port := (($state.Values.listeners.admin.port | int) | int) -}}
{{- if (gt (($listener.port | int) | int) (1 | int)) -}}
{{- $port = (($listener.port | int) | int) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts $i) | int) -}}
{{- else -}}{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts (0 | int)) | int) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $port) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedSchemaPort" -}}
{{- $state := (index .a 0) -}}
{{- $i := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $keys := (keys $state.Values.listeners.schemaRegistry.external) -}}
{{- $_ := (sortAlpha $keys) -}}
{{- $externalSchemaListenerName := (first $keys) -}}
{{- $listener := (ternary (index $state.Values.listeners.schemaRegistry.external (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $externalSchemaListenerName)))) "r")) (dict "enabled" (coalesce nil) "advertisedPorts" (coalesce nil) "port" 0 "nodePort" (coalesce nil) "tls" (coalesce nil)) (hasKey $state.Values.listeners.schemaRegistry.external (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $externalSchemaListenerName)))) "r"))) -}}
{{- $port := (($state.Values.listeners.schemaRegistry.port | int) | int) -}}
{{- if (gt (($listener.port | int) | int) (1 | int)) -}}
{{- $port = (($listener.port | int) | int) -}}
{{- end -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts $i) | int) -}}
{{- else -}}{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $listener.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = ((index $listener.advertisedPorts (0 | int)) | int) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $port) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedHost" -}}
{{- $state := (index .a 0) -}}
{{- $i := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $address := (printf "%s-%d" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") ($i | int)) -}}
{{- if (ne (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") "") -}}
{{- $address = (printf "%s.%s" $address (tpl $state.Values.external.domain $state.Dot)) -}}
{{- end -}}
{{- if (le ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (0 | int)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $address) | toJson -}}
{{- break -}}
{{- end -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (1 | int)) -}}
{{- $address = (index $state.Values.external.addresses (0 | int)) -}}
{{- else -}}
{{- $address = (index $state.Values.external.addresses $i) -}}
{{- end -}}
{{- if (ne (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") "") -}}
{{- $address = (printf "%s.%s" $address (tpl $state.Values.external.domain $state.Dot)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $address) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.getFirstExternalKafkaListener" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $keys := (keys $state.Values.listeners.kafka.external) -}}
{{- $_ := (sortAlpha $keys) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" (first $keys))))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.BrokerList" -}}
{{- $state := (index .a 0) -}}
{{- $port := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $bl := (get (fromJson (include "redpanda.brokersFor" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" ""))) (dict "Statefulset" $state.Values.statefulset)) $port)))) "r") -}}
{{- range $_, $set := $state.Pools -}}
{{- $bl = (concat (default (list) $bl) (default (list) (get (fromJson (include "redpanda.brokersFor" (dict "a" (list $state $set $port)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $bl) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.brokersFor" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- $port := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $bl := (coalesce nil) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($pool.Statefulset.replicas | int)|int) (1|int) -}}
{{- if (eq $port -1) -}}
{{- $bl = (concat (default (list) $bl) (list (printf "%s%s-%d.%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r") $i (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")))) -}}
{{- else -}}
{{- $bl = (concat (default (list) $bl) (list (printf "%s%s-%d.%s:%d" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r") $i (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r") $port))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $bl) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkNodeConfig" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokerList := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.kafka.port | int))))) "r") -}}
{{- $adminTLS := (coalesce nil) -}}
{{- $tls_6 := (get (fromJson (include "redpanda.rpkAdminAPIClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_6)))) "r") | int) (0 | int)) -}}
{{- $adminTLS = $tls_6 -}}
{{- end -}}
{{- $brokerTLS := (coalesce nil) -}}
{{- $tls_7 := (get (fromJson (include "redpanda.rpkKafkaClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_7)))) "r") | int) (0 | int)) -}}
{{- $brokerTLS = $tls_7 -}}
{{- end -}}
{{- $schemaRegistryTLS := (coalesce nil) -}}
{{- $tls_8 := (get (fromJson (include "redpanda.rpkSchemaRegistryClientTLSConfiguration" (dict "a" (list $state)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_8)))) "r") | int) (0 | int)) -}}
{{- $schemaRegistryTLS = $tls_8 -}}
{{- end -}}
{{- $_405_lockMemory_overprovisioned_flags := (get (fromJson (include "redpanda.RedpandaAdditionalStartFlags" (dict "a" (list $state.Values $pool)))) "r") -}}
{{- $lockMemory := (index $_405_lockMemory_overprovisioned_flags 0) -}}
{{- $overprovisioned := (index $_405_lockMemory_overprovisioned_flags 1) -}}
{{- $flags := (index $_405_lockMemory_overprovisioned_flags 2) -}}
{{- $result := (dict "additional_start_flags" $flags "enable_memory_locking" $lockMemory "overprovisioned" $overprovisioned "kafka_api" (dict "brokers" $brokerList "tls" $brokerTLS) "admin_api" (dict "addresses" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.admin.port | int))))) "r") "tls" $adminTLS) "schema_registry" (dict "addresses" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.schemaRegistry.port | int))))) "r") "tls" $schemaRegistryTLS)) -}}
{{- $result = (merge (dict) $result (get (fromJson (include "redpanda.Tuning.Translate" (dict "a" (list $state.Values.tuning)))) "r")) -}}
{{- $result = (merge (dict) $result (get (fromJson (include "redpanda.Config.CreateRPKConfiguration" (dict "a" (list $state.Values.config)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkKafkaClientTLSConfiguration" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tls := $state.Values.listeners.kafka.tls -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $result := (dict "ca_file" (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- if $tls.requireClientAuth -}}
{{- $_ := (set $result "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- $_ := (set $result "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkAdminAPIClientTLSConfiguration" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tls := $state.Values.listeners.admin.tls -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $result := (dict "ca_file" (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- if $tls.requireClientAuth -}}
{{- $_ := (set $result "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- $_ := (set $result "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkSchemaRegistryClientTLSConfiguration" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $tls := $state.Values.listeners.schemaRegistry.tls -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $result := (dict "ca_file" (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $tls $state.Values.tls)))) "r")) -}}
{{- if $tls.requireClientAuth -}}
{{- $_ := (set $result "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- $_ := (set $result "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $tls $state.Values.tls)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.kafkaClient" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokerList := (list) -}}
{{- range $_, $broker := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state -1)))) "r") -}}
{{- $brokerList = (concat (default (list) $brokerList) (list (dict "address" $broker "port" ($state.Values.listeners.kafka.port | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $kafkaTLS := $state.Values.listeners.kafka.tls -}}
{{- $brokerTLS := (coalesce nil) -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.kafka.tls $state.Values.tls)))) "r") -}}
{{- $brokerTLS = (dict "enabled" true "require_client_auth" $kafkaTLS.requireClientAuth "truststore_file" (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $kafkaTLS $state.Values.tls)))) "r")) -}}
{{- if $kafkaTLS.requireClientAuth -}}
{{- $_ := (set $brokerTLS "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $kafkaTLS $state.Values.tls)))) "r"))) -}}
{{- $_ := (set $brokerTLS "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $kafkaTLS $state.Values.tls)))) "r"))) -}}
{{- end -}}
{{- end -}}
{{- $cfg := (dict "brokers" $brokerList) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $brokerTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $cfg "broker_tls" $brokerTLS) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.configureListeners" -}}
{{- $redpanda := (index .a 0) -}}
{{- $state := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $defaultKafkaAuth := (coalesce nil) -}}
{{- if $state.Values.auth.sasl.enabled -}}
{{- $defaultKafkaAuth = "sasl" -}}
{{- end -}}
{{- $_ := (set $redpanda "admin" (get (fromJson (include "redpanda.ListenerConfig.Listeners" (dict "a" (list $state.Values.listeners.admin (coalesce nil))))) "r")) -}}
{{- $_ := (set $redpanda "kafka_api" (get (fromJson (include "redpanda.ListenerConfig.Listeners" (dict "a" (list $state.Values.listeners.kafka $defaultKafkaAuth)))) "r")) -}}
{{- $_ := (set $redpanda "rpc_server" (get (fromJson (include "redpanda.rpcListeners" (dict "a" (list $state)))) "r")) -}}
{{- $_ := (set $redpanda "admin_api_tls" (coalesce nil)) -}}
{{- $tls_9 := (get (fromJson (include "redpanda.ListenerConfig.ListenersTLS" (dict "a" (list $state.Values.listeners.admin $state.Values.tls)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_9)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $redpanda "admin_api_tls" $tls_9) -}}
{{- end -}}
{{- $_ := (set $redpanda "kafka_api_tls" (coalesce nil)) -}}
{{- $tls_10 := (get (fromJson (include "redpanda.ListenerConfig.ListenersTLS" (dict "a" (list $state.Values.listeners.kafka $state.Values.tls)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_10)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $redpanda "kafka_api_tls" $tls_10) -}}
{{- end -}}
{{- $tls_11 := (get (fromJson (include "redpanda.rpcListenersTLS" (dict "a" (list $state)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_11)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $redpanda "rpc_server_tls" $tls_11) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.pandaProxyListener" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $pandaProxy := (dict) -}}
{{- $pandaProxyAuth := (coalesce nil) -}}
{{- if (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $pandaProxyAuth = "http_basic" -}}
{{- end -}}
{{- $_ := (set $pandaProxy "pandaproxy_api" (get (fromJson (include "redpanda.ListenerConfig.Listeners" (dict "a" (list $state.Values.listeners.http $pandaProxyAuth)))) "r")) -}}
{{- $_ := (set $pandaProxy "pandaproxy_api_tls" (coalesce nil)) -}}
{{- $tls_12 := (get (fromJson (include "redpanda.ListenerConfig.ListenersTLS" (dict "a" (list $state.Values.listeners.http $state.Values.tls)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_12)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $pandaProxy "pandaproxy_api_tls" $tls_12) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $pandaProxy) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.schemaRegistry" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $schemaReg := (dict) -}}
{{- $_ := (set $schemaReg "schema_registry_api" (get (fromJson (include "redpanda.ListenerConfig.Listeners" (dict "a" (list $state.Values.listeners.schemaRegistry (coalesce nil))))) "r")) -}}
{{- $_ := (set $schemaReg "schema_registry_api_tls" (coalesce nil)) -}}
{{- $tls_13 := (get (fromJson (include "redpanda.ListenerConfig.ListenersTLS" (dict "a" (list $state.Values.listeners.schemaRegistry $state.Values.tls)))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $tls_13)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $schemaReg "schema_registry_api_tls" $tls_13) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $schemaReg) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpcListenersTLS" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $r := $state.Values.listeners.rpc -}}
{{- if (and (not ((or (or (get (fromJson (include "redpanda.RedpandaAtLeast_22_2_atleast_22_2_10" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.RedpandaAtLeast_22_3_atleast_22_3_13" (dict "a" (list $state)))) "r")) (get (fromJson (include "redpanda.RedpandaAtLeast_23_1_2" (dict "a" (list $state)))) "r")))) ((or (and (eq (toJson $r.tls.enabled) "null") $state.Values.tls.enabled) (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $r.tls.enabled false)))) "r")))) -}}
{{- $_ := (fail (printf "Redpanda version v%s does not support TLS on the RPC port. Please upgrade. See technical service bulletin 2023-01." (trimPrefix "v" (get (fromJson (include "redpanda.Tag" (dict "a" (list $state)))) "r")))) -}}
{{- end -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $r.tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "enabled" true "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ServerMountPoint" (dict "a" (list $r.tls $state.Values.tls)))) "r")) "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ServerMountPoint" (dict "a" (list $r.tls $state.Values.tls)))) "r")) "require_client_auth" $r.tls.requireClientAuth "truststore_file" (get (fromJson (include "redpanda.InternalTLS.TrustStoreFilePath" (dict "a" (list $r.tls $state.Values.tls)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpcListeners" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "address" "0.0.0.0" "port" ($state.Values.listeners.rpc.port | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.createInternalListenerTLSCfg" -}}
{{- $tls := (index .a 0) -}}
{{- $internal := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $internal $tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "name" "internal" "enabled" true "cert_file" (printf "%s/tls.crt" (get (fromJson (include "redpanda.InternalTLS.ServerMountPoint" (dict "a" (list $internal $tls)))) "r")) "key_file" (printf "%s/tls.key" (get (fromJson (include "redpanda.InternalTLS.ServerMountPoint" (dict "a" (list $internal $tls)))) "r")) "require_client_auth" $internal.requireClientAuth "truststore_file" (get (fromJson (include "redpanda.InternalTLS.TrustStoreFilePath" (dict "a" (list $internal $tls)))) "r"))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaAdditionalStartFlags" -}}
{{- $values := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $flags := (get (fromJson (include "redpanda.RedpandaResources.GetRedpandaFlags" (dict "a" (list $values.resources)))) "r") -}}
{{- $_ := (set $flags "--default-log-level" $values.logging.logLevel) -}}
{{- if (eq (index $values.config.node "developer_mode") true) -}}
{{- $_ := (unset $flags "--reserve-memory") -}}
{{- end -}}
{{- range $key, $value := (get (fromJson (include "chartutil.ParseFlags" (dict "a" (list $pool.Statefulset.additionalRedpandaCmdFlags)))) "r") -}}
{{- $_ := (set $flags $key $value) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $enabledOptions := (dict "true" true "1" true "" true) -}}
{{- $lockMemory := false -}}
{{- $_671_value_14_ok_15 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $flags "--lock-memory" "")))) "r") -}}
{{- $value_14 := (index $_671_value_14_ok_15 0) -}}
{{- $ok_15 := (index $_671_value_14_ok_15 1) -}}
{{- if $ok_15 -}}
{{- $lockMemory = (ternary (index $enabledOptions $value_14) false (hasKey $enabledOptions $value_14)) -}}
{{- $_ := (unset $flags "--lock-memory") -}}
{{- end -}}
{{- $overprovisioned := false -}}
{{- $_678_value_16_ok_17 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $flags "--overprovisioned" "")))) "r") -}}
{{- $value_16 := (index $_678_value_16_ok_17 0) -}}
{{- $ok_17 := (index $_678_value_16_ok_17 1) -}}
{{- if $ok_17 -}}
{{- $overprovisioned = (ternary (index $enabledOptions $value_16) false (hasKey $enabledOptions $value_16)) -}}
{{- $_ := (unset $flags "--overprovisioned") -}}
{{- end -}}
{{- $keys := (keys $flags) -}}
{{- $keys = (sortAlpha $keys) -}}
{{- $rendered := (coalesce nil) -}}
{{- range $_, $key := $keys -}}
{{- $value := (ternary (index $flags $key) "" (hasKey $flags $key)) -}}
{{- if (eq $value "") -}}
{{- $rendered = (concat (default (list) $rendered) (list $key)) -}}
{{- else -}}
{{- $rendered = (concat (default (list) $rendered) (list (printf "%s=%s" $key $value))) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $lockMemory $overprovisioned $rendered)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

