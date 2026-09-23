{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart/notes.go" */ -}}

{{- define "redpanda.Notes" -}}
{{- $dot := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $state := (mustMergeOverwrite (dict "Release" (coalesce nil) "Files" (coalesce nil) "Chart" (coalesce nil) "Values" (dict "nameOverride" "" "fullnameOverride" "" "clusterDomain" "" "commonLabels" (coalesce nil) "commonAnnotations" (coalesce nil) "image" (dict "repository" "" "tag" "") "service" (coalesce nil) "license_key" "" "auditLogging" (dict "enabled" false "listener" "" "partitions" 0 "enabledEventTypes" (coalesce nil) "excludedTopics" (coalesce nil) "excludedPrincipals" (coalesce nil) "clientMaxBufferSize" 0 "queueDrainIntervalMs" 0 "queueMaxBufferSizePerShard" 0 "replicationFactor" 0) "enterprise" (dict "license" "") "rackAwareness" (dict "enabled" false "nodeAnnotation" "") "console" (dict) "auth" (dict "sasl" (coalesce nil)) "tls" (dict "enabled" false "certs" (coalesce nil)) "external" (dict "addresses" (coalesce nil) "annotations" (coalesce nil) "domain" (coalesce nil) "enabled" false "type" "" "prefixTemplate" "" "sourceRanges" (coalesce nil) "service" (dict "enabled" false) "externalDns" (coalesce nil)) "logging" (dict "logLevel" "" "usageStats" (dict "enabled" false "clusterId" (coalesce nil))) "monitoring" (dict "enabled" false "scrapeInterval" "" "labels" (coalesce nil) "tlsConfig" (coalesce nil) "enableHttp2" (coalesce nil)) "resources" (dict "cpu" (dict "cores" "0" "overprovisioned" (coalesce nil)) "memory" (dict "enable_memory_locking" (coalesce nil) "container" (dict "min" (coalesce nil) "max" "0") "redpanda" (coalesce nil))) "storage" (dict "hostPath" "" "tiered" (dict "credentialsSecretRef" (dict "accessKey" (coalesce nil) "secretKey" (coalesce nil)) "config" (coalesce nil) "hostPath" "" "mountType" "" "persistentVolume" (dict "annotations" (coalesce nil) "enabled" false "labels" (coalesce nil) "nameOverwrite" "" "size" "" "storageClass" "")) "persistentVolume" (coalesce nil) "tieredConfig" (coalesce nil) "tieredStorageHostPath" "" "tieredStoragePersistentVolume" (coalesce nil)) "post_install_job" (dict "enabled" false "labels" (coalesce nil) "annotations" (coalesce nil) "podTemplate" (dict)) "statefulset" (dict "additionalSelectorLabels" (coalesce nil) "replicas" 0 "updateStrategy" (dict) "additionalRedpandaCmdFlags" (coalesce nil) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0 "custom" (coalesce nil)) "sideCars" (dict "image" (dict "repository" "" "tag" "") "args" (coalesce nil) "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "image" (coalesce nil) "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "" "run" (coalesce nil))) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "serviceAccount" (dict "annotations" (coalesce nil) "create" false "name" "") "rbac" (dict "enabled" false "rpkDebugBundle" false "annotations" (coalesce nil)) "tuning" (dict) "listeners" (dict "admin" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "http" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "kafka" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "schemaRegistry" (dict "enabled" false "external" (coalesce nil) "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil))) "rpc" (dict "port" 0 "tls" (dict "enabled" (coalesce nil) "cert" "" "requireClientAuth" false "trustStore" (coalesce nil)))) "config" (dict "cluster" (coalesce nil) "extraClusterConfiguration" (coalesce nil) "node" (coalesce nil) "rpk" (coalesce nil) "schema_registry_client" (coalesce nil) "pandaproxy_client" (coalesce nil) "tunable" (coalesce nil)) "tests" (coalesce nil) "force" false "podTemplate" (dict)) "BootstrapUserSecret" (coalesce nil) "BootstrapUserPassword" "" "StatefulSetPodLabels" (coalesce nil) "StatefulSetSelector" (coalesce nil) "Pools" (coalesce nil) "Dot" (coalesce nil) "ViaOperator" false "CloudEnvironment" "" "OperatorVersion" "") (dict "Release" $dot.Release "Files" $dot.Files "Chart" $dot.Chart "Values" $dot.Values.AsMap "Dot" $dot)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (concat (default (list) (get (fromJson (include "redpanda.warnings" (dict "a" (list $state)))) "r")) (default (list) (get (fromJson (include "redpanda.notes" (dict "a" (list $state)))) "r")))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.warnings" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $out := (coalesce nil) -}}
{{- $w_1 := (get (fromJson (include "redpanda.cpuWarning" (dict "a" (list $state)))) "r") -}}
{{- if (ne $w_1 "") -}}
{{- $out = (concat (default (list) $out) (list (printf `**Warning**: %s` $w_1))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.cpuWarning" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $coresInMillis := ((get (fromJson (include "_shims.resource_MilliValue" (dict "a" (list $state.Values.resources.cpu.cores)))) "r") | int64) -}}
{{- if (lt $coresInMillis (1000 | int64)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%dm is below the minimum recommended CPU value for Redpanda" $coresInMillis)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.notes" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $anySASL := (get (fromJson (include "redpanda.Auth.IsSASLEnabled" (dict "a" (list $state.Values.auth)))) "r") -}}
{{- $out := (coalesce nil) -}}
{{- $out = (concat (default (list) $out) (list `` `` `` `` (printf `Congratulations on installing %s!` $state.Chart.Name) `` `The pods will rollout in a few seconds. To check the status:` `` (printf `  kubectl -n %s rollout status statefulset %s --watch` $state.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")))) -}}
{{- if (and $state.Values.external.enabled (eq $state.Values.external.type "LoadBalancer")) -}}
{{- $out = (concat (default (list) $out) (list `` `If you are using the load balancer service with a cloud provider, the services will likely have automatically-generated addresses. In this scenario the advertised listeners must be updated in order for external access to work. Run the following command once Redpanda is deployed:` `` (printf `  helm upgrade %s redpanda/redpanda --reuse-values -n %s --set $(kubectl get svc -n %s -o jsonpath='{"external.addresses={"}{ range .items[*]}{.status.loadBalancer.ingress[0].ip }{.status.loadBalancer.ingress[0].hostname}{","}{ end }{"}\n"}')` (get (fromJson (include "redpanda.Name" (dict "a" (list $state)))) "r") $state.Release.Namespace $state.Release.Namespace))) -}}
{{- end -}}
{{- $profiles := (keys $state.Values.listeners.kafka.external) -}}
{{- $profiles = (sortAlpha $profiles) -}}
{{- $profileName := (index $profiles (0 | int)) -}}
{{- $out = (concat (default (list) $out) (list `` `Set up rpk for access to your external listeners:`)) -}}
{{- $profile := (ternary (index $state.Values.listeners.kafka.external $profileName) (dict "enabled" (coalesce nil) "advertisedPorts" (coalesce nil) "port" 0 "nodePort" (coalesce nil) "tls" (coalesce nil)) (hasKey $state.Values.listeners.kafka.external $profileName)) -}}
{{- if (get (fromJson (include "redpanda.ExternalTLS.IsEnabled" (dict "a" (list $profile.tls $state.Values.listeners.kafka.tls $state.Values.tls)))) "r") -}}
{{- $external := "" -}}
{{- if (and (ne (toJson $profile.tls) "null") (ne (toJson $profile.tls.cert) "null")) -}}
{{- $external = $profile.tls.cert -}}
{{- else -}}
{{- $external = $state.Values.listeners.kafka.tls.cert -}}
{{- end -}}
{{- $out = (concat (default (list) $out) (list (printf `  kubectl get secret -n %s %s-%s-cert -o go-template='{{ index .data "ca.crt" | base64decode }}' > ca.crt` $state.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $external))) -}}
{{- if (or $state.Values.listeners.kafka.tls.requireClientAuth $state.Values.listeners.admin.tls.requireClientAuth) -}}
{{- $out = (concat (default (list) $out) (list (printf `  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.crt" | base64decode }}' > tls.crt` $state.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) (printf `  kubectl get secret -n %s %s-client -o go-template='{{ index .data "tls.key" | base64decode }}' > tls.key` $state.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")))) -}}
{{- end -}}
{{- end -}}
{{- $out = (concat (default (list) $out) (list (printf `  rpk profile create --from-profile <(kubectl get configmap -n %s %s-rpk -o go-template='{{ .data.profile }}') %s` $state.Release.Namespace (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") $profileName) `` `Set up dns to look up the pods on their Kubernetes Nodes. You can use this query to get the list of short-names to IP addresses. Add your external domain to the hostnames and you could test by adding these to your /etc/hosts:` `` (printf `  kubectl get pod -n %s -o custom-columns=node:.status.hostIP,name:.metadata.name --no-headers -l app.kubernetes.io/name=redpanda,app.kubernetes.io/component=redpanda-statefulset` $state.Release.Namespace))) -}}
{{- if $anySASL -}}
{{- $out = (concat (default (list) $out) (list `` `Set the credentials in the environment:` `` (printf `  kubectl -n %s get secret %s -o go-template="{{ range .data }}{{ . | base64decode }}{{ end }}" | IFS=: read -r %s` $state.Release.Namespace $state.Values.auth.sasl.secretRef "RPK_USER RPK_PASS RPK_SASL_MECHANISM") (printf `  export %s` "RPK_USER RPK_PASS RPK_SASL_MECHANISM"))) -}}
{{- end -}}
{{- $out = (concat (default (list) $out) (list `` `Try some sample commands:`)) -}}
{{- if $anySASL -}}
{{- $out = (concat (default (list) $out) (list `Create a user:` `` (printf `  rpk acl user create myuser --new-password changeme --mechanism %s` (get (fromJson (include "redpanda.SASLAuth.GetMechanism" (dict "a" (list $state.Values.auth.sasl)))) "r")) `` `Give the user permissions:` `` `  rpk acl create --allow-principal 'myuser' --allow-host '*' --operation all --topic 'test-topic'`)) -}}
{{- end -}}
{{- $out = (concat (default (list) $out) (list `` `Get the api status:` `` `  rpk cluster info` `` `Create a topic` `` (printf `  rpk topic create test-topic -p 3 -r %d` (min (3 | int64) (($state.Values.statefulset.replicas | int) | int64))) `` `Describe the topic:` `` `  rpk topic describe test-topic` `` `Delete the topic:` `` `  rpk topic delete test-topic`)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $out) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

