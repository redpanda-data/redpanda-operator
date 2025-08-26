# redpanda

![Version: 25.1.1-beta3](https://img.shields.io/badge/Version-25.1.1--beta3-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v25.2.1](https://img.shields.io/badge/AppVersion-v25.2.1-informational?style=flat-square)

Redpanda is the real-time engine for modern apps.

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| redpanda-data |  | <https://github.com/orgs/redpanda-data/people> |

## Source Code

* <https://github.com/redpanda-data/redpanda-operator/tree/main/charts/redpanda>

## Requirements

Kubernetes: `>= 1.25.0-0`

| Repository | Name | Version |
|------------|------|---------|
| file://../../console | console | >=3.1.0-0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| auditLogging | object | `{"clientMaxBufferSize":16777216,"enabled":false,"enabledEventTypes":null,"excludedPrincipals":null,"excludedTopics":null,"listener":"internal","partitions":12,"queueDrainIntervalMs":500,"queueMaxBufferSizePerShard":1048576,"replicationFactor":null}` | Audit logging for a redpanda cluster, must have enabled sasl and have one kafka listener supporting sasl authentication for audit logging to work. Note this feature is only available for redpanda versions >= v23.3.0. |
| auditLogging.clientMaxBufferSize | int | `16777216` | Defines the number of bytes (in bytes) allocated by the internal audit client for audit messages. |
| auditLogging.enabled | bool | `false` | Enable or disable audit logging, for production clusters we suggest you enable, however, this will only work if you also enable sasl and a listener with sasl enabled. |
| auditLogging.enabledEventTypes | string | `nil` | Event types that should be captured by audit logs, default is [`admin`, `authenticate`, `management`]. |
| auditLogging.excludedPrincipals | string | `nil` | List of principals to exclude from auditing, default is null. |
| auditLogging.excludedTopics | string | `nil` | List of topics to exclude from auditing, default is null. |
| auditLogging.listener | string | `"internal"` | Kafka listener name, note that it must have `authenticationMethod` set to `sasl`. For external listeners, use the external listener name, such as `default`. |
| auditLogging.partitions | int | `12` | Integer value defining the number of partitions used by a newly created audit topic. |
| auditLogging.queueDrainIntervalMs | int | `500` | In ms, frequency in which per shard audit logs are batched to client for write to audit log. |
| auditLogging.queueMaxBufferSizePerShard | int | `1048576` | Defines the maximum amount of memory used (in bytes) by the audit buffer in each shard. |
| auditLogging.replicationFactor | string | `nil` | Defines the replication factor for a newly created audit log topic. This configuration applies only to the audit log topic and may be different from the cluster or other topic configurations. This cannot be altered for existing audit log topics. Setting this value is optional. If a value is not provided, Redpanda will use the `internal_topic_replication_factor cluster` config value. Default is `null` |
| auth | object | `{"sasl":{"bootstrapUser":{"mechanism":"SCRAM-SHA-256"},"enabled":false,"mechanism":"SCRAM-SHA-512","secretRef":"redpanda-users","users":[]}}` | Authentication settings. For details, see the [SASL documentation](https://docs.redpanda.com/docs/manage/kubernetes/security/sasl-kubernetes/). |
| auth.sasl.bootstrapUser | object | `{"mechanism":"SCRAM-SHA-256"}` | Details about how to create the bootstrap user for the cluster. The secretKeyRef is optionally specified. If it is specified, the chart will use a password written to that secret when creating the "kubernetes-controller" bootstrap user. If it is unspecified, then the secret will be generated and stored in the secret "releasename"-bootstrap-user, with the key "password". |
| auth.sasl.bootstrapUser.mechanism | string | `"SCRAM-SHA-256"` | The authentication mechanism to use for the bootstrap user. Options are `SCRAM-SHA-256` and `SCRAM-SHA-512`. |
| auth.sasl.enabled | bool | `false` | Enable SASL authentication. If you enable SASL authentication, you must provide a Secret in `auth.sasl.secretRef`. |
| auth.sasl.mechanism | string | `"SCRAM-SHA-512"` | The authentication mechanism to use for the superuser. Options are `SCRAM-SHA-256` and `SCRAM-SHA-512`. |
| auth.sasl.secretRef | string | `"redpanda-users"` | A Secret that contains your superuser credentials. For details, see the [SASL documentation](https://docs.redpanda.com/docs/manage/kubernetes/security/sasl-kubernetes/#use-secrets). |
| auth.sasl.users | list | `[]` | Optional list of superusers. These superusers will be created in the Secret whose name is defined in `auth.sasl.secretRef`. If this list is empty, the Secret in `auth.sasl.secretRef` must already exist in the cluster before you deploy the chart. Uncomment the sample list if you wish to try adding sample sasl users or override to use your own. |
| clusterDomain | string | `"cluster.local."` | Default Kubernetes cluster domain. |
| commonLabels | object | `{}` | Additional labels to add to all Kubernetes objects. For example, `my.k8s.service: redpanda`. |
| config | object | `{"cluster":{},"extraClusterConfiguration":{},"node":{"crash_loop_limit":5},"pandaproxy_client":{},"rpk":{},"schema_registry_client":{},"tunable":{"compacted_log_segment_size":67108864,"kafka_connection_rate_limit":1000,"log_segment_size_max":268435456,"log_segment_size_min":16777216,"max_compacted_log_segment_size":536870912}}` | This section contains various settings supported by Redpanda that may not work correctly in a Kubernetes cluster. Changing these settings comes with some risk.  Use these settings to customize various Redpanda configurations that are not covered in other sections. These values have no impact on the configuration or behavior of the Kubernetes objects deployed by Helm, and therefore should not be modified for the purpose of configuring those objects. Instead, these settings get passed directly to the Redpanda binary at startup. For descriptions of these properties, see the [configuration documentation](https://docs.redpanda.com/docs/cluster-administration/configuration/). |
| config.cluster | object | `{}` | [Cluster Configuration Properties](https://docs.redpanda.com/current/reference/properties/cluster-properties/) |
| config.node | object | `{"crash_loop_limit":5}` | [Broker (node) Configuration Properties](https://docs.redpanda.com/docs/reference/broker-properties/). |
| config.node.crash_loop_limit | int | `5` | Crash loop limit A limit on the number of consecutive times a broker can crash within one hour before its crash-tracking logic is reset. This limit prevents a broker from getting stuck in an infinite cycle of crashes. User can disable this crash loop limit check by the following action:  * One hour elapses since the last crash * The node configuration file, redpanda.yaml, is updated via config.cluster or config.node or config.tunable objects * The startup_log file in the node’s data_directory is manually deleted  Default to 5 REF: https://docs.redpanda.com/current/reference/broker-properties/#crash_loop_limit |
| config.tunable | object | `{"compacted_log_segment_size":67108864,"kafka_connection_rate_limit":1000,"log_segment_size_max":268435456,"log_segment_size_min":16777216,"max_compacted_log_segment_size":536870912}` | Tunable cluster properties. Deprecated: all settings here may be specified via `config.cluster`. |
| config.tunable.compacted_log_segment_size | int | `67108864` | See the [property reference documentation](https://docs.redpanda.com/docs/reference/cluster-properties/#compacted_log_segment_size). |
| config.tunable.kafka_connection_rate_limit | int | `1000` | See the [property reference documentation](https://docs.redpanda.com/docs/reference/cluster-properties/#kafka_connection_rate_limit). |
| config.tunable.log_segment_size_max | int | `268435456` | See the [property reference documentation](https://docs.redpanda.com/docs/reference/cluster-properties/#log_segment_size_max). |
| config.tunable.log_segment_size_min | int | `16777216` | See the [property reference documentation](https://docs.redpanda.com/docs/reference/cluster-properties/#log_segment_size_min). |
| config.tunable.max_compacted_log_segment_size | int | `536870912` | See the [property reference documentation](https://docs.redpanda.com/docs/reference/cluster-properties/#max_compacted_log_segment_size). |
| console | object | `{"config":{},"configmap":{"create":false},"deployment":{"create":false},"enabled":true,"secret":{"create":false}}` | Redpanda Console settings. For a reference of configuration settings, see the [Redpanda Console documentation](https://docs.redpanda.com/docs/reference/console/config/). |
| enterprise | object | `{"license":"","licenseSecretRef":null}` | Enterprise (optional) For details, see the [License documentation](https://docs.redpanda.com/docs/get-started/licenses/?platform=kubernetes#redpanda-enterprise-edition). |
| enterprise.license | string | `""` | license (optional). |
| enterprise.licenseSecretRef | string | `nil` | Secret name and key where the license key is stored. |
| external | object | `{"enabled":true,"service":{"enabled":true},"type":"NodePort"}` | External access settings. For details, see the [Networking and Connectivity documentation](https://docs.redpanda.com/docs/manage/kubernetes/networking/networking-and-connectivity/). |
| external.enabled | bool | `true` | Enable external access for each Service. You can toggle external access for each listener in `listeners.<service name>.external.<listener-name>.enabled`. |
| external.service | object | `{"enabled":true}` | Service allows you to manage the creation of an external kubernetes service object |
| external.service.enabled | bool | `true` | Enabled if set to false will not create the external service type You can still set your cluster with external access but not create the supporting service (NodePort/LoadBalander). Set this to false if you rather manage your own service. |
| external.type | string | `"NodePort"` | External access type. Only `NodePort` and `LoadBalancer` are supported. If undefined, then advertised listeners will be configured in Redpanda, but the helm chart will not create a Service. You must create a Service manually. Warning: If you use LoadBalancers, you will likely experience higher latency and increased packet loss. NodePort is recommended in cases where latency is a priority. |
| fullnameOverride | string | `""` | Override `redpanda.fullname` template. |
| image | object | `{"repository":"docker.redpanda.com/redpandadata/redpanda","tag":""}` | Redpanda Docker image settings. |
| image.repository | string | `"docker.redpanda.com/redpandadata/redpanda"` | Docker repository from which to pull the Redpanda Docker image. |
| image.tag | string | `Chart.appVersion`. | The Redpanda version. See DockerHub for: [All stable versions](https://hub.docker.com/r/redpandadata/redpanda/tags) and [all unstable versions](https://hub.docker.com/r/redpandadata/redpanda-unstable/tags). |
| listeners | object | `{"admin":{"external":{"default":{"advertisedPorts":[31644],"port":9645,"tls":{"cert":"external"}}},"port":9644,"tls":{"cert":"default","requireClientAuth":false}},"http":{"authenticationMethod":null,"enabled":true,"external":{"default":{"advertisedPorts":[30082],"authenticationMethod":null,"port":8083,"tls":{"cert":"external","requireClientAuth":false}}},"port":8082,"tls":{"cert":"default","requireClientAuth":false}},"kafka":{"authenticationMethod":null,"external":{"default":{"advertisedPorts":[31092],"authenticationMethod":null,"port":9094,"tls":{"cert":"external"}}},"port":9093,"tls":{"cert":"default","requireClientAuth":false}},"rpc":{"port":33145,"tls":{"cert":"default","requireClientAuth":false}},"schemaRegistry":{"authenticationMethod":null,"enabled":true,"external":{"default":{"advertisedPorts":[30081],"authenticationMethod":null,"port":8084,"tls":{"cert":"external","requireClientAuth":false}}},"port":8081,"tls":{"cert":"default","requireClientAuth":false}}}` | Listener settings.  Override global settings configured above for individual listeners. For details, see the [listeners documentation](https://docs.redpanda.com/docs/manage/kubernetes/networking/configure-listeners/). |
| listeners.admin | object | `{"external":{"default":{"advertisedPorts":[31644],"port":9645,"tls":{"cert":"external"}}},"port":9644,"tls":{"cert":"default","requireClientAuth":false}}` | Admin API listener (only one). |
| listeners.admin.external | object | `{"default":{"advertisedPorts":[31644],"port":9645,"tls":{"cert":"external"}}}` | Optional external access settings. |
| listeners.admin.external.default | object | `{"advertisedPorts":[31644],"port":9645,"tls":{"cert":"external"}}` | Name of the external listener. |
| listeners.admin.external.default.tls | object | `{"cert":"external"}` | The port advertised to this listener's external clients. List one port if you want to use the same port for each broker (would be the case when using NodePort service). Otherwise, list the port you want to use for each broker in order of StatefulSet replicas. If undefined, `listeners.admin.port` is used. |
| listeners.admin.port | int | `9644` | The port for both internal and external connections to the Admin API. |
| listeners.admin.tls | object | `{"cert":"default","requireClientAuth":false}` | Optional TLS section (required if global TLS is enabled) |
| listeners.admin.tls.cert | string | `"default"` | Name of the Certificate used for TLS (must match a Certificate name that is registered in tls.certs). |
| listeners.admin.tls.requireClientAuth | bool | `false` | If true, the truststore file for this listener is included in the ConfigMap. |
| listeners.http | object | `{"authenticationMethod":null,"enabled":true,"external":{"default":{"advertisedPorts":[30082],"authenticationMethod":null,"port":8083,"tls":{"cert":"external","requireClientAuth":false}}},"port":8082,"tls":{"cert":"default","requireClientAuth":false}}` | HTTP API listeners (aka PandaProxy). |
| listeners.kafka | object | `{"authenticationMethod":null,"external":{"default":{"advertisedPorts":[31092],"authenticationMethod":null,"port":9094,"tls":{"cert":"external"}}},"port":9093,"tls":{"cert":"default","requireClientAuth":false}}` | Kafka API listeners. |
| listeners.kafka.external.default.advertisedPorts | list | `[31092]` | If undefined, `listeners.kafka.external.default.port` is used. |
| listeners.kafka.external.default.port | int | `9094` | The port used for external client connections. |
| listeners.kafka.port | int | `9093` | The port for internal client connections. |
| listeners.rpc | object | `{"port":33145,"tls":{"cert":"default","requireClientAuth":false}}` | RPC listener (this is never externally accessible). |
| listeners.schemaRegistry | object | `{"authenticationMethod":null,"enabled":true,"external":{"default":{"advertisedPorts":[30081],"authenticationMethod":null,"port":8084,"tls":{"cert":"external","requireClientAuth":false}}},"port":8081,"tls":{"cert":"default","requireClientAuth":false}}` | Schema registry listeners. |
| logging | object | `{"logLevel":"info","usageStats":{"enabled":true}}` | Log-level settings. |
| logging.logLevel | string | `"info"` | Log level Valid values (from least to most verbose) are: `warn`, `info`, `debug`, and `trace`. |
| logging.usageStats | object | `{"enabled":true}` | Send usage statistics back to Redpanda Data. For details, see the [stats reporting documentation](https://docs.redpanda.com/docs/cluster-administration/monitoring/#stats-reporting). |
| monitoring | object | `{"enabled":false,"labels":{},"scrapeInterval":"30s"}` | Monitoring. This will create a ServiceMonitor that can be used by Prometheus-Operator or VictoriaMetrics-Operator to scrape the metrics. |
| nameOverride | string | `""` | Override `redpanda.name` template. |
| podTemplate.annotations | object | `{}` | Annotations to apply (or overwrite the default) to all Pods of this Chart. |
| podTemplate.labels | object | `{}` | Labels to apply (or overwrite the default) to all Pods of this Chart. |
| podTemplate.spec | object | `{"imagePullSecrets":[],"securityContext":{"fsGroup":101,"fsGroupChangePolicy":"OnRootMismatch","runAsUser":101}}` | A subset of Kubernetes' PodSpec type that will be merged into the PodSpec of all Pods for this Chart. See [Merge Semantics](#merging-semantics) for details. |
| podTemplate.spec.imagePullSecrets | list | `[]` | Pull secrets may be used to provide credentials to image repositories See the [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/). |
| post_install_job.enabled | bool | `true` |  |
| post_install_job.podTemplate.annotations | object | `{}` | Annotations to apply (or overwrite the default) to the Pods of this Job. |
| post_install_job.podTemplate.labels | object | `{}` | Labels to apply (or overwrite the default) to the Pods of this Job. |
| post_install_job.podTemplate.spec | object | `{"containers":[{"env":[],"name":"post-install","securityContext":{}}],"securityContext":{}}` | A subset of Kubernetes' PodSpec type that will be merged into the final PodSpec. See [Merge Semantics](#merging-semantics) for details. |
| rackAwareness | object | `{"enabled":false,"nodeAnnotation":"topology.kubernetes.io/zone"}` | Rack Awareness settings. For details, see the [Rack Awareness documentation](https://docs.redpanda.com/docs/manage/kubernetes/kubernetes-rack-awareness/). |
| rackAwareness.enabled | bool | `false` | When running in multiple racks or availability zones, use a Kubernetes Node annotation value as the Redpanda rack value. Enabling this requires running with a service account with "get" Node permissions. To have the Helm chart configure these permissions, set `serviceAccount.create=true` and `rbac.enabled=true`. |
| rackAwareness.nodeAnnotation | string | `"topology.kubernetes.io/zone"` | The common well-known annotation to use as the rack ID. Override this only if you use a custom Node annotation. |
| rbac | object | `{"annotations":{},"enabled":true,"rpkDebugBundle":true}` | Role Based Access Control. |
| rbac.annotations | object | `{}` | Annotations to add to the `rbac` resources. |
| rbac.enabled | bool | `true` | Controls whether or not Roles, ClusterRoles, and bindings thereof will be generated. Disabling this very likely result in a non-functional deployment. |
| rbac.rpkDebugBundle | bool | `true` | Controls whether or not a Role and RoleBinding will be generated for the permissions required by `rpk debug bundle`. Disabling will not affect the redpanda deployment itself but a bundle is required to engage with our support. |
| resources | object | `{"cpu":{"cores":1},"memory":{"container":{"max":"2.5Gi"}}}` | Pod resource management.
This section simplifies resource allocation for the redpanda container by
providing a single location where resources are defined.

Resources may be specified by either setting `resources.cpu` and
`resources.memory` (the default) or by setting `resources.requests` and
`resources.limits`.

For details on `resources.cpu` and `resources.memory`, see their respective
documentation below.

When `resources.limits` and `resources.requests` are set, the redpanda
container's resources will be set to exactly the provided values. This allows
users to granularly control limits and requests to best suit their use case.
For example: `resources.requests.cpu` may be set without setting
`resources.limits.cpu` to avoid the potential of CPU throttling.

Redpanda's resource related CLI flags will then be calculated as follows:
* `--smp max(1, floor(coalesce(resources.requests.cpu, resources.limits.cpu)))`
* `--memory coalesce(resources.requests.memory, resources.limits.memory) * 90%`
* `--reserve-memory 0`
* `--overprovisioned coalesce(resources.requests.cpu, resources.limits.cpu) < 1000m`

If neither a request nor a limit is provided for cpu or memory, the
corresponding flag will be omitted. As a result, setting `resources.limits`
and `resources.requests` to `{}` will result in redpanda being run without
`--smp` or `--memory`. (This is not recommended).

If the computed CLI flags are undesirable, they may be overridden by
specifying the desired value through `statefulset.additionalRedpandaCmdFlags`.

The default values are for a development environment.
Production-level values and other considerations are documented,
where those values are different from the default.
For details,
see the [Pod resources documentation](https://docs.redpanda.com/docs/manage/kubernetes/manage-resources/). |
| resources.cpu | object | `{"cores":1}` | CPU resources. For details, see the [Pod resources documentation](https://docs.redpanda.com/docs/manage/kubernetes/manage-resources/#configure-cpu-resources). |
| resources.cpu.cores | int | `1` | Redpanda makes use of a thread per core model. For details, see this [blog](https://redpanda.com/blog/tpc-buffers). For this reason, Redpanda should only be given full cores.  Note: You can increase cores, but decreasing cores is supported only from 24.3 Redpanda version.  This setting is equivalent to `--smp`, `resources.requests.cpu`, and `resources.limits.cpu`. For production, use `4` or greater.  To maximize efficiency, use the `static` CPU manager policy by specifying an even integer for CPU resource requests and limits. This policy gives the Pods running Redpanda brokers access to exclusive CPUs on the node. See https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/#static-policy. |
| resources.memory | object | `{"container":{"max":"2.5Gi"}}` | Memory resources For details, see the [Pod resources documentation](https://docs.redpanda.com/docs/manage/kubernetes/manage-resources/#configure-memory-resources). |
| resources.memory.container | object | `{"max":"2.5Gi"}` | Enables memory locking. For production, set to `true`. enable_memory_locking: false  It is recommended to have at least 2Gi of memory per core for the Redpanda binary. This memory is taken from the total memory given to each container. The Helm chart allocates 80% of the container's memory to Redpanda, leaving the rest for other container processes. So at least 2.5Gi per core is recommended in order to ensure Redpanda has a full 2Gi.  These values affect `--memory` and `--reserve-memory` flags passed to Redpanda and the memory requests/limits in the StatefulSet. Valid suffixes: k, M, G, T, P, Ki, Mi, Gi, Ti, Pi To create `Guaranteed` Pod QoS for Redpanda brokers, provide both container max and min values for the container. For details, see https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/#create-a-pod-that-gets-assigned-a-qos-class-of-guaranteed * Every container in the Pod must have a memory limit and a memory request. * For every container in the Pod, the memory limit must equal the memory request.  |
| resources.memory.container.max | string | `"2.5Gi"` | Maximum memory count for each Redpanda broker. Equivalent to `resources.limits.memory`. For production, use `10Gi` or greater. |
| serviceAccount | object | `{"annotations":{},"create":true,"name":""}` | Service account management. |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `""` | The name of the service account to use. If not set and `serviceAccount.create` is `true`, a name is generated using the `redpanda.fullname` template. |
| statefulset.additionalRedpandaCmdFlags | list | `[]` | Additional flags to pass to redpanda, |
| statefulset.additionalSelectorLabels | object | `{}` | Additional labels to be added to statefulset label selector. For example, `my.k8s.service: redpanda`. |
| statefulset.budget.maxUnavailable | int | `1` |  |
| statefulset.initContainerImage.repository | string | `"busybox"` |  |
| statefulset.initContainerImage.tag | string | `"latest"` |  |
| statefulset.initContainers.configurator.additionalCLIArgs | list | `[]` |  |
| statefulset.initContainers.fsValidator.enabled | bool | `false` |  |
| statefulset.initContainers.fsValidator.expectedFS | string | `"xfs"` |  |
| statefulset.initContainers.setDataDirOwnership.enabled | bool | `false` | In environments where root is not allowed, you cannot change the ownership of files and directories. Enable `setDataDirOwnership` when using default minikube cluster configuration. |
| statefulset.podAntiAffinity.custom | object | `{}` | Change `podAntiAffinity.type` to `custom` and provide your own podAntiAffinity rules here. |
| statefulset.podAntiAffinity.topologyKey | string | `"kubernetes.io/hostname"` | The topologyKey to be used. Can be used to spread across different nodes, AZs, regions etc. |
| statefulset.podAntiAffinity.type | string | `"hard"` | Valid anti-affinity types are `soft`, `hard`, or `custom`. Use `custom` if you want to supply your own anti-affinity rules in the `podAntiAffinity.custom` object. |
| statefulset.podAntiAffinity.weight | int | `100` | Weight for `soft` anti-affinity rules. Does not apply to other anti-affinity types. |
| statefulset.podTemplate.annotations | object | `{}` | Additional annotations to apply to the Pods of the StatefulSet. |
| statefulset.podTemplate.labels | object | `{}` | Additional labels to apply to the Pods of the StatefulSet. |
| statefulset.podTemplate.spec | object | `{"affinity":{"podAntiAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":[{"labelSelector":{"matchLabels":{"app.kubernetes.io/component":"{{ include \"redpanda.name\" . }}-statefulset","app.kubernetes.io/instance":"{{ .Release.Name }}","app.kubernetes.io/name":"{{ include \"redpanda.name\" . }}"}},"topologyKey":"kubernetes.io/hostname"}]}},"nodeSelector":{},"priorityClassName":"","securityContext":{},"terminationGracePeriodSeconds":90,"tolerations":[],"topologySpreadConstraints":[{"labelSelector":{"matchLabels":{"app.kubernetes.io/component":"{{ include \"redpanda.name\" . }}-statefulset","app.kubernetes.io/instance":"{{ .Release.Name }}","app.kubernetes.io/name":"{{ include \"redpanda.name\" . }}"}},"maxSkew":1,"topologyKey":"topology.kubernetes.io/zone","whenUnsatisfiable":"ScheduleAnyway"}]}` | A subset of Kubernetes' PodSpec type that will be merged into the final PodSpec. See [Merge Semantics](#merging-semantics) for details. |
| statefulset.podTemplate.spec.nodeSelector | object | `{}` | Node selection constraints for scheduling Pods of this StatefulSet. These constraints override the global `nodeSelector` value. For details, see the [Kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector). |
| statefulset.podTemplate.spec.priorityClassName | string | `""` | PriorityClassName given to Pods of this StatefulSet. For details, see the [Kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass). |
| statefulset.podTemplate.spec.terminationGracePeriodSeconds | int | `90` | Termination grace period in seconds is time required to execute preStop hook which puts particular Redpanda Pod (process/container) into maintenance mode. Before settle down on particular value please put Redpanda under load and perform rolling upgrade or rolling restart. That value needs to accommodate two processes: * preStop hook needs to put Redpanda into maintenance mode * after preStop hook Redpanda needs to handle gracefully SIGTERM signal  Both processes are executed sequentially where preStop hook has hard deadline in the middle of terminationGracePeriodSeconds.  REF: https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/#hook-handler-execution https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination |
| statefulset.podTemplate.spec.tolerations | list | `[]` | Taints to be tolerated by Pods of this StatefulSet. These tolerations override the global tolerations value. For details, see the [Kubernetes documentation](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/). |
| statefulset.replicas | int | `3` | Number of Redpanda brokers (Redpanda Data recommends setting this to the number of worker nodes in the cluster) |
| statefulset.sideCars.brokerDecommissioner.decommissionAfter | string | `"60s"` |  |
| statefulset.sideCars.brokerDecommissioner.decommissionRequeueTimeout | string | `"10s"` |  |
| statefulset.sideCars.brokerDecommissioner.enabled | bool | `false` |  |
| statefulset.sideCars.configWatcher.enabled | bool | `true` |  |
| statefulset.sideCars.controllers.createRBAC | bool | `true` |  |
| statefulset.sideCars.controllers.enabled | bool | `false` |  |
| statefulset.sideCars.controllers.healthProbeAddress | string | `":8085"` |  |
| statefulset.sideCars.controllers.metricsAddress | string | `":9082"` |  |
| statefulset.sideCars.controllers.pprofAddress | string | `":9083"` |  |
| statefulset.sideCars.controllers.run[0] | string | `"all"` |  |
| statefulset.sideCars.image.repository | string | `"docker.redpanda.com/redpandadata/redpanda-operator"` |  |
| statefulset.sideCars.image.tag | string | `"v25.2.1-beta1"` |  |
| statefulset.sideCars.pvcUnbinder.enabled | bool | `false` |  |
| statefulset.sideCars.pvcUnbinder.unbindAfter | string | `"60s"` |  |
| statefulset.updateStrategy.type | string | `"RollingUpdate"` |  |
| storage | object | `{"hostPath":"","persistentVolume":{"annotations":{},"enabled":true,"labels":{},"nameOverwrite":"","size":"20Gi","storageClass":""},"tiered":{"config":{"cloud_storage_cache_size":5368709120,"cloud_storage_enable_remote_read":true,"cloud_storage_enable_remote_write":true,"cloud_storage_enabled":false},"credentialsSecretRef":{"accessKey":{"configurationKey":"cloud_storage_access_key"},"secretKey":{"configurationKey":"cloud_storage_secret_key"}},"hostPath":"","mountType":"none","persistentVolume":{"annotations":{},"labels":{},"storageClass":""}}}` | Persistence settings. For details, see the [storage documentation](https://docs.redpanda.com/docs/manage/kubernetes/configure-storage/). |
| storage.hostPath | string | `""` | Absolute path on the host to store Redpanda's data. If unspecified, then an `emptyDir` volume is used. If specified but `persistentVolume.enabled` is true, `storage.hostPath` has no effect. |
| storage.persistentVolume | object | `{"annotations":{},"enabled":true,"labels":{},"nameOverwrite":"","size":"20Gi","storageClass":""}` | If `persistentVolume.enabled` is true, a PersistentVolumeClaim is created and used to store Redpanda's data. Otherwise, `storage.hostPath` is used. |
| storage.persistentVolume.annotations | object | `{}` | Additional annotations to apply to the created PersistentVolumeClaims. |
| storage.persistentVolume.labels | object | `{}` | Additional labels to apply to the created PersistentVolumeClaims. |
| storage.persistentVolume.nameOverwrite | string | `""` | Option to change volume claim template name for tiered storage persistent volume if tiered.mountType is set to `persistentVolume` |
| storage.persistentVolume.storageClass | string | `""` | To disable dynamic provisioning, set to `-`. If undefined or empty (default), then no storageClassName spec is set, and the default dynamic provisioner is chosen (gp2 on AWS, standard on GKE, AWS & OpenStack). |
| storage.tiered.config | object | `{"cloud_storage_cache_size":5368709120,"cloud_storage_enable_remote_read":true,"cloud_storage_enable_remote_write":true,"cloud_storage_enabled":false}` | Tiered Storage settings Requires `enterprise.licenseKey` or `enterprised.licenseSecretRef` For details, see the [Tiered Storage documentation](https://docs.redpanda.com/docs/manage/kubernetes/tiered-storage/). For a list of properties, see [Object Storage Properties](https://docs.redpanda.com/current/reference/properties/object-storage-properties/). |
| storage.tiered.config.cloud_storage_cache_size | int | `5368709120` | Maximum size of the disk cache used by Tiered Storage. Default is 20 GiB. See the [property reference documentation](https://docs.redpanda.com/docs/reference/object-storage-properties/#cloud_storage_cache_size). |
| storage.tiered.config.cloud_storage_enable_remote_read | bool | `true` | Cluster level default remote read configuration for new topics. See the [property reference documentation](https://docs.redpanda.com/docs/reference/object-storage-properties/#cloud_storage_enable_remote_read). |
| storage.tiered.config.cloud_storage_enable_remote_write | bool | `true` | Cluster level default remote write configuration for new topics. See the [property reference documentation](https://docs.redpanda.com/docs/reference/object-storage-properties/#cloud_storage_enable_remote_write). |
| storage.tiered.config.cloud_storage_enabled | bool | `false` | Global flag that enables Tiered Storage if a license key is provided. See the [property reference documentation](https://docs.redpanda.com/docs/reference/object-storage-properties/#cloud_storage_enabled). |
| storage.tiered.hostPath | string | `""` | Absolute path on the host to store Redpanda's Tiered Storage cache. |
| storage.tiered.persistentVolume.annotations | object | `{}` | Additional annotations to apply to the created PersistentVolumeClaims. |
| storage.tiered.persistentVolume.labels | object | `{}` | Additional labels to apply to the created PersistentVolumeClaims. |
| storage.tiered.persistentVolume.storageClass | string | `""` | To disable dynamic provisioning, set to "-". If undefined or empty (default), then no storageClassName spec is set, and the default dynamic provisioner is chosen (gp2 on AWS, standard on GKE, AWS & OpenStack). |
| tests.enabled | bool | `true` |  |
| tls | object | `{"certs":{"default":{"caEnabled":true},"external":{"caEnabled":true}},"enabled":true}` | TLS settings. For details, see the [TLS documentation](https://docs.redpanda.com/docs/manage/kubernetes/security/kubernetes-tls/). |
| tls.certs | object | `{"default":{"caEnabled":true},"external":{"caEnabled":true}}` | List all Certificates here, then you can reference a specific Certificate's name in each listener's `listeners.<listener name>.tls.cert` setting. |
| tls.certs.default | object | `{"caEnabled":true}` | This key is the Certificate name. To apply the Certificate to a specific listener, reference the Certificate's name in `listeners.<listener-name>.tls.cert`. |
| tls.certs.default.caEnabled | bool | `true` | Indicates whether or not the Secret holding this certificate includes a `ca.crt` key. When `true`, chart managed clients, such as rpk, will use `ca.crt` for certificate verification and listeners with `require_client_auth` and no explicit `truststore` will use `ca.crt` as their `truststore_file` for verification of client certificates. When `false`, chart managed clients will use `tls.crt` for certificate verification and listeners with `require_client_auth` and no explicit `truststore` will use the container's CA certificates. |
| tls.certs.external | object | `{"caEnabled":true}` | Example external tls configuration uncomment and set the right key to the listeners that require them also enable the tls setting for those listeners. |
| tls.certs.external.caEnabled | bool | `true` | Indicates whether or not the Secret holding this certificate includes a `ca.crt` key. When `true`, chart managed clients, such as rpk, will use `ca.crt` for certificate verification and listeners with `require_client_auth` and no explicit `truststore` will use `ca.crt` as their `truststore_file` for verification of client certificates. When `false`, chart managed clients will use `tls.crt` for certificate verification and listeners with `require_client_auth` and no explicit `truststore` will use the container's CA certificates. |
| tls.enabled | bool | `true` | Enable TLS globally for all listeners. Each listener must include a Certificate name in its `<listener>.tls` object. To allow you to enable TLS for individual listeners, Certificates in `auth.tls.certs` are always loaded, even if `tls.enabled` is `false`. See `listeners.<listener-name>.tls.enabled`. |
| tuning | object | `{"tune_aio_events":true}` | Redpanda tuning settings. Each is set to their default values in Redpanda. |
| tuning.tune_aio_events | bool | `true` | Increase the maximum number of outstanding asynchronous IO operations if the current value is below a certain threshold. This allows Redpanda to make as many simultaneous IO requests as possible, increasing throughput.  When this option is enabled, Helm creates a privileged container. If your security profile does not allow this, you can disable this container by setting `tune_aio_events` to `false`. For more details, see the [tuning documentation](https://docs.redpanda.com/docs/deploy/deployment-option/self-hosted/kubernetes/kubernetes-tune-workers/). |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
