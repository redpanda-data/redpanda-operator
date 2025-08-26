{{- /* Generated from "render_state.go" */ -}}

{{- define "redpanda.RenderState.FetchBootstrapUser" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (or (eq (toJson $r.Values.auth.sasl) "null") (not $r.Values.auth.sasl.enabled)) (ne (toJson $r.Values.auth.sasl.bootstrapUser.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secretName := (printf "%s-bootstrap-user" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r")) -}}
{{- $_97_existing_1_ok_2 := (get (fromJson (include "_shims.lookup" (dict "a" (list "v1" "Secret" $r.Release.Namespace $secretName)))) "r") -}}
{{- $existing_1 := (index $_97_existing_1_ok_2 0) -}}
{{- $ok_2 := (index $_97_existing_1_ok_2 1) -}}
{{- if $ok_2 -}}
{{- $_ := (set $existing_1 "immutable" true) -}}
{{- $_ := (set $r "BootstrapUserSecret" $existing_1) -}}
{{- $selector := (get (fromJson (include "redpanda.BootstrapUser.SecretKeySelector" (dict "a" (list $r.Values.auth.sasl.bootstrapUser (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $_114_data_3_found_4 := (get (fromJson (include "_shims.dicttest" (dict "a" (list $existing_1.data $selector.key (coalesce nil))))) "r") -}}
{{- $data_3 := (index $_114_data_3_found_4 0) -}}
{{- $found_4 := (index $_114_data_3_found_4 1) -}}
{{- if $found_4 -}}
{{- $_ := (set $r "BootstrapUserPassword" (toString $data_3)) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.RenderState.FetchStatefulSetPodSelector" -}}
{{- $r := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if $r.Release.IsUpgrade -}}
{{- $_129_existing_5_ok_6 := (get (fromJson (include "_shims.lookup" (dict "a" (list "apps/v1" "StatefulSet" $r.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $r)))) "r"))))) "r") -}}
{{- $existing_5 := (index $_129_existing_5_ok_6 0) -}}
{{- $ok_6 := (index $_129_existing_5_ok_6 1) -}}
{{- if (and $ok_6 (gt ((get (fromJson (include "_shims.len" (dict "a" (list $existing_5.spec.template.metadata.labels)))) "r") | int) (0 | int))) -}}
{{- $_ := (set $r "StatefulSetPodLabels" $existing_5.spec.template.metadata.labels) -}}
{{- $_ := (set $r "StatefulSetSelector" $existing_5.spec.selector.matchLabels) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.renderStateFromDot" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $state := (mustMergeOverwrite (dict "Release" (dict "Name" "" "Namespace" "" "Service" "" "IsUpgrade" false "IsInstall" false) "Files" (dict) "Chart" (dict "Name" "" "Version" "" "AppVersion" "") "Values" (dict "nameOverride" "" "fullnameOverride" "" "clusterDomain" "" "commonLabels" (coalesce nil) "image" (dict "repository" "" "tag" "") "service" (coalesce nil) "license_key" "" "auditLogging" (dict "enabled" false "listener" "" "partitions" 0 "enabledEventTypes" (coalesce nil) "excludedTopics" (coalesce nil) "excludedPrincipals" (coalesce nil) "clientMaxBufferSize" 0 "queueDrainIntervalMs" 0 "queueMaxBufferSizePerShard" 0 "replicationFactor" 0) "enterprise" (dict "license" "") "rackAwareness" (dict "enabled" false "nodeAnnotation" "") "console" (dict) "auth" (dict "sasl" (coalesce nil)) "tls" (dict "enabled" false "certs" (coalesce nil)) "external" (dict "addresses" (coalesce nil) "annotations" (coalesce nil) "domain" (coalesce nil) "enabled" false "type" "" "prefixTemplate" "" "sourceRanges" (coalesce nil) "service" (dict "enabled" false) "externalDns" (coalesce nil)) "logging" (dict "logLevel" "" "usageStats" (dict "enabled" false "clusterId" (coalesce nil))) "monitoring" (dict "enabled" false "scrapeInterval" "" "labels" (coalesce nil) "tlsConfig" (coalesce nil) "enableHttp2" (coalesce nil)) "resources" (dict "cpu" (dict "cores" "0" "overprovisioned" (coalesce nil)) "memory" (dict "enable_memory_locking" (coalesce nil) "container" (dict "min" (coalesce nil) "max" "0") "redpanda" (coalesce nil))) "storage" (dict "hostPath" "" "tiered" (dict "credentialsSecretRef" (dict "accessKey" (coalesce nil) "secretKey" (coalesce nil)) "config" (coalesce nil) "hostPath" "" "mountType" "" "persistentVolume" (dict "annotations" (coalesce nil) "enabled" false "labels" (coalesce nil) "nameOverwrite" "" "size" "" "storageClass" "")) "persistentVolume" (coalesce nil) "tieredConfig" (coalesce nil) "tieredStorageHostPath" "" "tieredStoragePersistentVolume" (coalesce nil)) "post_install_job" (dict "enabled" false "labels" (coalesce nil) "annotations" (coalesce nil) "podTemplate" (dict)) "statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "serviceAccount" (dict "annotations" (coalesce nil) "create" false "name" "") "rbac" (dict "enabled" false "rpkDebugBundle" false "annotations" (coalesce nil)) "tuning" (dict) "listeners" (dict "admin" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "http" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "kafka" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "schemaRegistry" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "rpc" (dict "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) "config" (dict "cluster" (coalesce nil) "extraClusterConfiguration" (coalesce nil) "node" (coalesce nil) "rpk" (coalesce nil) "schema_registry_client" (coalesce nil) "pandaproxy_client" (coalesce nil) "tunable" (coalesce nil)) "tests" (coalesce nil) "force" false "podTemplate" (dict)) "BootstrapUserSecret" (coalesce nil) "BootstrapUserPassword" "" "StatefulSetPodLabels" (coalesce nil) "StatefulSetSelector" (coalesce nil) "Pools" (coalesce nil)) (dict "Release" $dot.Release "Files" $dot.Files "Chart" $dot.Chart "Values" $dot.Values.AsMap)) -}}
{{- $_ := (get (fromJson (include "redpanda.RenderState.FetchBootstrapUser" (dict "a" (list $state)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.RenderState.FetchStatefulSetPodSelector" (dict "a" (list $state)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

