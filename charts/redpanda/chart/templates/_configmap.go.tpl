{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/configmap.tpl.go" */ -}}

{{- define "redpanda.ConfigMaps" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $cms := (list (get (fromJson (include "redpanda.RedpandaConfigMap" (dict "a" (list $state $listeners (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r")) -}}
{{- range $_, $set := $state.Pools -}}
{{- $cms = (concat (default (list) $cms) (list (get (fromJson (include "redpanda.RedpandaConfigMap" (dict "a" (list $state $listeners $set)))) "r"))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) $cms) (list (get (fromJson (include "redpanda.RPKProfile" (dict "a" (list $state $listeners)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaConfigMap" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- $pool := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_37_bootstrap_fixups := (get (fromJson (include "redpanda.BootstrapFile" (dict "a" (list $state $pool)))) "r") -}}
{{- $bootstrap := (index $_37_bootstrap_fixups 0) -}}
{{- $fixups := (index $_37_bootstrap_fixups 1) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "kind" "ConfigMap" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "data" (dict ".bootstrap.json.in" $bootstrap "bootstrap.yaml.fixups" $fixups "redpanda.yaml" (get (fromJson (include "redpanda.RedpandaConfigFile" (dict "a" (list $state $listeners true $pool)))) "r"))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.BootstrapFile" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_69_template_fixups := (get (fromJson (include "redpanda.BootstrapContents" (dict "a" (list $state $pool)))) "r") -}}
{{- $template := (index $_69_template_fixups 0) -}}
{{- $fixups := (index $_69_template_fixups 1) -}}
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
{{- $_93_attrs_fixes := (get (fromJson (include "redpanda.TieredStorageConfig.Translate" (dict "a" (list (deepCopy (get (fromJson (include "redpanda.Storage.GetTieredStorageConfig" (dict "a" (list $state.Values.storage)))) "r")) $state.Values.storage.tiered.credentialsSecretRef)))) "r") -}}
{{- $attrs := (index $_93_attrs_fixes 0) -}}
{{- $fixes := (index $_93_attrs_fixes 1) -}}
{{- range $k, $v := $attrs -}}
{{- $_101___ok_1 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $bootstrap $k (coalesce nil))))) "r") -}}
{{- $_ := (index $_101___ok_1 0) -}}
{{- $ok_1 := (index $_101___ok_1 1) -}}
{{- if (not $ok_1) -}}
{{- $_ := (set $bootstrap $k $v) -}}
{{- end -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $fixups = (concat (default (list) $fixups) (default (list) $fixes)) -}}
{{- $_114___ok_2 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $state.Values.config.cluster "default_topic_replications" (coalesce nil))))) "r") -}}
{{- $_ := (index $_114___ok_2 0) -}}
{{- $ok_2 := (index $_114___ok_2 1) -}}
{{- if (and (not $ok_2) (ge ($pool.Statefulset.replicas | int) (3 | int))) -}}
{{- $_ := (set $bootstrap "default_topic_replications" (3 | int)) -}}
{{- end -}}
{{- $_119___ok_3 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $state.Values.config.cluster "storage_min_free_bytes" (coalesce nil))))) "r") -}}
{{- $_ := (index $_119___ok_3 0) -}}
{{- $ok_3 := (index $_119___ok_3 1) -}}
{{- if (not $ok_3) -}}
{{- $_ := (set $bootstrap "storage_min_free_bytes" ((get (fromJson (include "redpanda.Storage.StorageMinFreeBytes" (dict "a" (list $state.Values.storage)))) "r") | int64)) -}}
{{- end -}}
{{- $template := (dict) -}}
{{- range $k, $v := $bootstrap -}}
{{- $_ := (set $template $k (toJson $v)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_130_extra_fixes__ := (get (fromJson (include "redpanda.ClusterConfiguration.Translate" (dict "a" (list (deepCopy $state.Values.config.extraClusterConfiguration))))) "r") -}}
{{- $extra := (index $_130_extra_fixes__ 0) -}}
{{- $fixes := (index $_130_extra_fixes__ 1) -}}
{{- $_ := (index $_130_extra_fixes__ 2) -}}
{{- $template = (merge (dict) $extra $template) -}}
{{- $fixups = (concat (default (list) $fixups) (default (list) $fixes)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list $template $fixups)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RedpandaConfigFile" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- $includeNonHashableItems := (index .a 2) -}}
{{- $pool := (index .a 3) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $redpandaConfig := (dict "empty_seed_starts_cluster" false) -}}
{{- if $includeNonHashableItems -}}
{{- $rpcPort := ((get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.RPC" (dict "a" (list $listeners)))) "r"))))) "r").Port | int) -}}
{{- $seeds := (coalesce nil) -}}
{{- range $_, $host := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state -1)))) "r") -}}
{{- $seeds = (concat (default (list) $seeds) (list (dict "host" (dict "address" $host "port" $rpcPort)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $redpandaConfig "seed_servers" $seeds) -}}
{{- end -}}
{{- $redpandaConfig = (merge (dict) $redpandaConfig (get (fromJson (include "redpanda.NodeConfig.Translate" (dict "a" (list $state.Values.config.node)))) "r")) -}}
{{- $sections := (get (fromJson (include "_redpanda.Listeners.ConfigSections" (dict "a" (list $listeners)))) "r") -}}
{{- range $_, $key := (sortAlpha (keys (index $sections "redpanda"))) -}}
{{- $_ := (set $redpandaConfig $key (index (index $sections "redpanda") $key)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $redpandaYaml := (dict "redpanda" $redpandaConfig "schema_registry" (index $sections "schema_registry") "pandaproxy" (index $sections "pandaproxy") "config_file" "/etc/redpanda/redpanda.yaml") -}}
{{- if $includeNonHashableItems -}}
{{- $_ := (set $redpandaYaml "rpk" (get (fromJson (include "redpanda.rpkNodeConfig" (dict "a" (list $state $listeners $pool)))) "r")) -}}
{{- $_ := (set $redpandaYaml "pandaproxy_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state $listeners "pandaproxy")))) "r")) -}}
{{- $_ := (set $redpandaYaml "schema_registry_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state $listeners "schema_registry")))) "r")) -}}
{{- if (and $state.Values.auditLogging.enabled (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r")) -}}
{{- $_ := (set $redpandaYaml "audit_log_client" (get (fromJson (include "redpanda.kafkaClient" (dict "a" (list $state $listeners "audit_log")))) "r")) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (toYaml $redpandaYaml)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RPKProfile" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $state.Values.external.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "kind" "ConfigMap" "apiVersion" "v1")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-rpk" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r") "annotations" (get (fromJson (include "redpanda.FullAnnotations" (dict "a" (list $state)))) "r"))) "data" (dict "profile" (toYaml (get (fromJson (include "redpanda.rpkProfile" (dict "a" (list $state $listeners)))) "r")))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.rpkProfile" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokerList := (list) -}}
{{- $adminAdvertisedList := (list) -}}
{{- $schemaAdvertisedList := (list) -}}
{{- range $_, $i := untilStep (((0 | int) | int)|int) (($state.Values.statefulset.replicas | int)|int) (1|int) -}}
{{- $host := (get (fromJson (include "redpanda.advertisedHost" (dict "a" (list $state $i)))) "r") -}}
{{- $brokerList = (concat (default (list) $brokerList) (list (printf "%s:%d" $host (((get (fromJson (include "_redpanda.API.ProfileAdvertisedPort" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Kafka" (dict "a" (list $listeners)))) "r") $i)))) "r") | int) | int)))) -}}
{{- $adminAdvertisedList = (concat (default (list) $adminAdvertisedList) (list (printf "%s:%d" $host (((get (fromJson (include "_redpanda.API.ProfileAdvertisedPort" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Admin" (dict "a" (list $listeners)))) "r") $i)))) "r") | int) | int)))) -}}
{{- $schemaAdvertisedList = (concat (default (list) $schemaAdvertisedList) (list (printf "%s:%d" $host (((get (fromJson (include "_redpanda.API.ProfileAdvertisedPort" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.SchemaRegistry" (dict "a" (list $listeners)))) "r") $i)))) "r") | int) | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $kafkaTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Kafka" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $kafkaTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $kafkaTLS "ca_file" "ca.crt") -}}
{{- end -}}
{{- $adminTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Admin" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $adminTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $adminTLS "ca_file" "ca.crt") -}}
{{- end -}}
{{- $schemaTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.SchemaRegistry" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $schemaTLS)))) "r") | int) (0 | int)) -}}
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
{{- $profileName := "" -}}
{{- range $name, $_ := $state.Values.listeners.kafka.external -}}
{{- $profileName = $name -}}
{{- break -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $result := (dict "name" $profileName "kafka_api" $ka "admin_api" $aa "schema_registry" $sa) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
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

{{- define "redpanda.BrokerList" -}}
{{- $state := (index .a 0) -}}
{{- $port := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $bl := (get (fromJson (include "redpanda.brokersFor" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)) $port)))) "r") -}}
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
{{- $listeners := (index .a 1) -}}
{{- $pool := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $kafka := (get (fromJson (include "_redpanda.API.InCluster" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Kafka" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- $brokerList := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($kafka.Port | int))))) "r") -}}
{{- $adminTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Admin" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- $brokerTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Kafka" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- $schemaRegistryTLS := (get (fromJson (include "_redpanda.API.RPKClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.SchemaRegistry" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- $_348_lockMemory_overprovisioned_flags := (get (fromJson (include "redpanda.RedpandaAdditionalStartFlags" (dict "a" (list $state.Values $pool)))) "r") -}}
{{- $lockMemory := (index $_348_lockMemory_overprovisioned_flags 0) -}}
{{- $overprovisioned := (index $_348_lockMemory_overprovisioned_flags 1) -}}
{{- $flags := (index $_348_lockMemory_overprovisioned_flags 2) -}}
{{- $result := (dict "additional_start_flags" $flags "enable_memory_locking" $lockMemory "overprovisioned" $overprovisioned "kafka_api" (dict "brokers" $brokerList "tls" $brokerTLS) "admin_api" (dict "addresses" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.admin.port | int))))) "r") "tls" $adminTLS) "schema_registry" (dict "addresses" (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state ($state.Values.listeners.schemaRegistry.port | int))))) "r") "tls" $schemaRegistryTLS)) -}}
{{- $result = (merge (dict) $result (get (fromJson (include "redpanda.Tuning.Translate" (dict "a" (list $state.Values.tuning)))) "r")) -}}
{{- $result = (merge (dict) $result (get (fromJson (include "redpanda.Config.CreateRPKConfiguration" (dict "a" (list $state.Values.config)))) "r")) -}}
{{- if $state.Values.tuning.apply_host_tuners -}}
{{- $result = (merge (dict) $result (get (fromJson (include "_redpanda.HostTunerDefaults" (dict "a" (list)))) "r")) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $result) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.kafkaClient" -}}
{{- $state := (index .a 0) -}}
{{- $listeners := (index .a 1) -}}
{{- $clientType := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $brokerList := (list) -}}
{{- $useLocalhostKey := (printf "%s_client.use_localhost" $clientType) -}}
{{- $useLocalhost := false -}}
{{- $_392_val_ok := (get (fromJson (include "_shims.dicttest" (dict "a" (list $state.Values.config.node $useLocalhostKey (coalesce nil))))) "r") -}}
{{- $val := (index $_392_val_ok 0) -}}
{{- $ok := (index $_392_val_ok 1) -}}
{{- if $ok -}}
{{- if (kindIs "bool" $val) -}}
{{- $useLocalhost = (eq $val true) -}}
{{- else -}}{{- if (kindIs "string" $val) -}}
{{- $strVal := (get (fromJson (include "_shims.typeassertion" (dict "a" (list "string" $val)))) "r") -}}
{{- $useLocalhost = (or (or (or (eq $strVal "true") (eq $strVal "True")) (eq $strVal "TRUE")) (eq $strVal "1")) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $useLocalhost -}}
{{- $brokerList = (concat (default (list) $brokerList) (list (dict "address" "localhost" "port" ($state.Values.listeners.kafka.port | int)))) -}}
{{- else -}}
{{- range $_, $broker := (get (fromJson (include "redpanda.BrokerList" (dict "a" (list $state -1)))) "r") -}}
{{- $brokerList = (concat (default (list) $brokerList) (list (dict "address" $broker "port" ($state.Values.listeners.kafka.port | int)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $brokerTLS := (get (fromJson (include "_redpanda.API.BrokerClientTLS" (dict "a" (list (get (fromJson (include "_redpanda.Listeners.Kafka" (dict "a" (list $listeners)))) "r"))))) "r") -}}
{{- $cfg := (dict "brokers" $brokerList) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $brokerTLS)))) "r") | int) (0 | int)) -}}
{{- $_ := (set $cfg "broker_tls" $brokerTLS) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $cfg) | toJson -}}
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
{{- $_456_value_4_ok_5 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $flags "--lock-memory" "")))) "r") -}}
{{- $value_4 := (index $_456_value_4_ok_5 0) -}}
{{- $ok_5 := (index $_456_value_4_ok_5 1) -}}
{{- if $ok_5 -}}
{{- $lockMemory = (ternary (index $enabledOptions $value_4) false (hasKey $enabledOptions $value_4)) -}}
{{- $_ := (unset $flags "--lock-memory") -}}
{{- end -}}
{{- $overprovisioned := false -}}
{{- $_463_value_6_ok_7 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $flags "--overprovisioned" "")))) "r") -}}
{{- $value_6 := (index $_463_value_6_ok_7 0) -}}
{{- $ok_7 := (index $_463_value_6_ok_7 1) -}}
{{- if $ok_7 -}}
{{- $overprovisioned = (ternary (index $enabledOptions $value_6) false (hasKey $enabledOptions $value_6)) -}}
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

