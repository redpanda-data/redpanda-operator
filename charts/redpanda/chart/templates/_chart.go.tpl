{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart.go" */ -}}

{{- define "redpanda.render" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $templater := (mustMergeOverwrite (dict "Dot" (coalesce nil)) (dict "Dot" $dot)) -}}
{{- $state := (mustMergeOverwrite (dict "Release" (coalesce nil) "Files" (coalesce nil) "Chart" (coalesce nil) "Values" (dict "nameOverride" "" "fullnameOverride" "" "clusterDomain" "" "image" (dict "repository" "" "tag" "") "auditLogging" (dict "enabled" false "listener" "" "partitions" 0 "clientMaxBufferSize" 0 "queueDrainIntervalMs" 0 "queueMaxBufferSizePerShard" 0) "enterprise" (dict "license" "") "rackAwareness" (dict "enabled" false "nodeAnnotation" "") "console" (dict) "auth" (dict) "tls" (dict "enabled" false) "external" (dict "enabled" false "type" "" "service" (dict "enabled" false)) "logging" (dict "logLevel" "" "usageStats" (dict "enabled" false)) "monitoring" (dict "enabled" false "scrapeInterval" "") "resources" (dict "cpu" (dict "cores" "0") "memory" (dict "container" (dict "max" "0"))) "storage" (dict "hostPath" "" "tiered" (dict "credentialsSecretRef" (dict) "hostPath" "" "mountType" "" "persistentVolume" (dict "storageClass" ""))) "post_install_job" (dict "enabled" false "podTemplate" (dict)) "statefulset" (dict "replicas" 0 "updateStrategy" (dict) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0) "sideCars" (dict "image" (dict "repository" "" "tag" "") "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "")) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "serviceAccount" (dict "create" false "name" "") "rbac" (dict "enabled" false "rpkDebugBundle" false) "tuning" (dict) "listeners" (dict "admin" (dict "port" 0 "tls" (dict "cert" "" "requireClientAuth" false)) "http" (dict "port" 0 "tls" (dict "cert" "" "requireClientAuth" false)) "kafka" (dict "port" 0 "tls" (dict "cert" "" "requireClientAuth" false)) "schemaRegistry" (dict "port" 0 "tls" (dict "cert" "" "requireClientAuth" false)) "rpc" (dict "port" 0 "tls" (dict "cert" "" "requireClientAuth" false))) "config" (dict) "tests" (dict "enabled" false) "podTemplate" (dict)) "BootstrapUserSecret" (coalesce nil) "BootstrapUserPassword" "" "StatefulSetPodLabels" (coalesce nil) "StatefulSetSelector" (coalesce nil) "Pools" (coalesce nil) "Dot" (coalesce nil) "Template" (coalesce nil) "ViaOperator" false "CloudEnvironment" "" "OperatorVersion" "") (dict "Release" $dot.Release "Files" $dot.Files "Chart" $dot.Chart "Values" $dot.Values.AsMap "Dot" $dot "Template" (list "redpanda.templater.Template" $templater))) -}}
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
{{- $_ := (get (fromJson (include "redpanda.ExternalConfig.ValidateGateway" (dict "a" (list $state.Values.external)))) "r") -}}
{{- $_ := (get (fromJson (include "redpanda.validateGatewayListeners" (dict "a" (list $state)))) "r") -}}
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
{{- range $_, $obj := (get (fromJson (include "redpanda.GatewayServices" (dict "a" (list $state)))) "r") -}}
{{- $manifests = (concat (default (list) $manifests) (list $obj)) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- range $_, $obj := (get (fromJson (include "redpanda.TLSRoutes" (dict "a" (list $state)))) "r") -}}
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
{{- if (and (not (get (fromJson (include "redpanda.RedpandaAtLeast_22_2_0" (dict "a" (list $state)))) "r")) (not (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.force false)))) "r"))) -}}
{{- $sv := (get (fromJson (include "redpanda.semver" (dict "a" (list $state)))) "r") -}}
{{- $_ := (fail (printf "Error: The Redpanda version (%s) is no longer supported \nTo accept this risk, run the upgrade again adding `--force=true`\n" $sv)) -}}
{{- end -}}
{{- end -}}
{{- end -}}

