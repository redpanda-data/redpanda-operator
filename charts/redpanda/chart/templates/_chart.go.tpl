{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart.go" */ -}}

{{- define "redpanda.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $state := (mustMergeOverwrite (dict "Release" (coalesce nil) "Files" (coalesce nil) "Chart" (coalesce nil) "Values" (dict "nameOverride" "" "fullnameOverride" "" "clusterDomain" "" "commonLabels" (coalesce nil) "image" (dict "repository" "" "tag" "") "service" (coalesce nil) "license_key" "" "auditLogging" (dict "enabled" false "listener" "" "partitions" 0 "enabledEventTypes" (coalesce nil) "excludedTopics" (coalesce nil) "excludedPrincipals" (coalesce nil) "clientMaxBufferSize" 0 "queueDrainIntervalMs" 0 "queueMaxBufferSizePerShard" 0 "replicationFactor" 0) "enterprise" (dict "license" "") "rackAwareness" (dict "enabled" false "nodeAnnotation" "") "console" (dict) "auth" (dict "sasl" (coalesce nil)) "tls" (dict "enabled" false "certs" (coalesce nil)) "external" (dict "addresses" (coalesce nil) "annotations" (coalesce nil) "domain" (coalesce nil) "enabled" false "type" "" "prefixTemplate" "" "sourceRanges" (coalesce nil) "service" (dict "enabled" false) "externalDns" (coalesce nil)) "logging" (dict "logLevel" "" "usageStats" (dict "enabled" false "clusterId" (coalesce nil))) "monitoring" (dict "enabled" false "scrapeInterval" "" "labels" (coalesce nil) "tlsConfig" (coalesce nil) "enableHttp2" (coalesce nil)) "resources" (dict "cpu" (dict "cores" "0" "overprovisioned" (coalesce nil)) "memory" (dict "enable_memory_locking" (coalesce nil) "container" (dict "min" (coalesce nil) "max" "0") "redpanda" (coalesce nil))) "storage" (dict "hostPath" "" "tiered" (dict "credentialsSecretRef" (dict "accessKey" (coalesce nil) "secretKey" (coalesce nil)) "config" (coalesce nil) "hostPath" "" "mountType" "" "persistentVolume" (dict "annotations" (coalesce nil) "enabled" false "labels" (coalesce nil) "nameOverwrite" "" "size" "" "storageClass" "")) "persistentVolume" (coalesce nil) "tieredConfig" (coalesce nil) "tieredStorageHostPath" "" "tieredStoragePersistentVolume" (coalesce nil)) "post_install_job" (dict "enabled" false "labels" (coalesce nil) "annotations" (coalesce nil) "podTemplate" (dict)) "statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "") "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "serviceAccount" (dict "annotations" (coalesce nil) "create" false "name" "") "rbac" (dict "enabled" false "rpkDebugBundle" false "annotations" (coalesce nil)) "tuning" (dict) "listeners" (dict "admin" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "http" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "kafka" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "schemaRegistry" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "rpc" (dict "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) "config" (dict "cluster" (coalesce nil) "extraClusterConfiguration" (coalesce nil) "node" (coalesce nil) "rpk" (coalesce nil) "schema_registry_client" (coalesce nil) "pandaproxy_client" (coalesce nil) "tunable" (coalesce nil)) "tests" (coalesce nil) "force" false "podTemplate" (dict)) "BootstrapUserSecret" (coalesce nil) "BootstrapUserPassword" "" "StatefulSetPodLabels" (coalesce nil) "StatefulSetSelector" (coalesce nil) "Pools" (coalesce nil) "Dot" (coalesce nil)) (dict "Release" $dot.Release "Files" $dot.Files "Chart" $dot.Chart "Values" $dot.Values.AsMap "Dot" $dot)) -}}
{{- $_ := (get (fromJson (include "redpanda.RenderState.FetchBootstrapUser" (dict "a" (list $state)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.RenderState.FetchStatefulSetPodSelector" (dict "a" (list $state)))) "r") -}}
{{- $manifests := (get (fromJson (include "redpanda.renderResources" (dict "a" (list $state)))) "r") -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.StatefulSets" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.renderResources" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_ := (get (fromJson (include "redpanda.checkVersion" (dict "a" (list $state)))) "r") -}}
{{- $manifests := (list (get (fromJson (include "redpanda.NodePortService" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.PodDisruptionBudget" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceAccount" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceInternal" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.ServiceMonitor" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.PostInstallUpgradeJob" (dict "a" (list $state)))) "r")) -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ConfigMaps" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.CertIssuers" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RootCAs" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClientCerts" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Roles" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoles" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.RoleBindings" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.ClusterRoleBindings" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.LoadBalancerServices" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.Secrets" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $manifests = (concat (default (list) $manifests) (default (list) (get (fromJson (include "redpanda.consoleChartIntegration" (dict "a" (list $state)))) "r"))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $manifests) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.checkVersion" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (not (get (fromJson (include "redpanda.RedpandaAtLeast_22_2_0" (dict "a" (list $state)))) "r")) (not $state.Values.force)) -}}
{{- $sv := (get (fromJson (include "redpanda.semver" (dict "a" (list $state)))) "r") -}}
{{- $_ := (fail (printf "Error: The Redpanda version (%s) is no longer supported \nTo accept this risk, run the upgrade again adding `--force=true`\n" $sv)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

