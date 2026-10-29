// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// Console defines the CRD for Redpanda Console instances.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=consoles
// +kubebuilder:storageversion
type Console struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda Console deployment.
	Spec ConsoleSpec `json:"spec,omitempty"`
	// Reports the observed state of the Deployment that runs Redpanda Console.
	Status ConsoleStatus `json:"status,omitempty"`
}

// GetClusterSource implements [ClusterReferencingObject].
func (c *Console) GetClusterSource() *ClusterSource {
	return c.Spec.ClusterSource
}

// UserList contains a list of Redpanda user objects.
// +kubebuilder:object:root=true
type ConsoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda user resources.
	Items []Console `json:"items"`
}

func (c *ConsoleList) GetItems() []*Console {
	return functional.MapFn(ptr.To, c.Items)
}

// ConsoleSpec defines the desired state of a Redpanda Console deployment.
// The operator renders these fields with the same logic as the Redpanda Console Helm chart, so fields left unset take the chart's default values.
// Writing Console resources is a privileged operation: they set the Console image, containers, volumes, and ServiceAccount,
// and with `pipelines.enabled` they make the operator create a ClusterRoleBinding to `system:auth-delegator`.
// +kubebuilder:validation:XValidation:message="pipelines.enabled requires a dedicated ServiceAccount for Console: the pipeline API accepts tokens of Console's ServiceAccount, and the namespace's default ServiceAccount is shared with every Pod that doesn't name one. Unset serviceAccount.name",rule="!has(self.pipelines) || !has(self.pipelines.enabled) || !self.pipelines.enabled || !has(self.serviceAccount) || !has(self.serviceAccount.name) || self.serviceAccount.name != 'default'"
type ConsoleSpec struct {
	ConsoleValues `json:",inline"`
	// Specifies the Redpanda cluster that Console connects to.
	// The operator generates the Kafka API, Admin API, and Schema Registry connection settings from this cluster and merges them into `config`.
	// If unset, you must configure the connection in `config` yourself.
	ClusterSource *ClusterSource `json:"cluster,omitempty"`
}

// ConsoleStatus reports the observed state of the Deployment that runs Redpanda Console.
type ConsoleStatus struct {
	// The generation observed by the Console controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,1,opt,name=observedGeneration"`

	// Total number of non-terminating Pods targeted by this Console's Deployment.
	// +optional
	Replicas int32 `json:"replicas,omitempty" protobuf:"varint,2,opt,name=replicas"`

	// Total number of non-terminating pods targeted by this Console's Deployment that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty" protobuf:"varint,3,opt,name=updatedReplicas"`

	// Total number of non-terminating pods targeted by this Console's Deployment with a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty" protobuf:"varint,7,opt,name=readyReplicas"`

	// Total number of available non-terminating pods (ready for at least minReadySeconds) targeted by this Console's Deployment.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty" protobuf:"varint,4,opt,name=availableReplicas"`

	// Total number of unavailable pods targeted by this deployment. This is the total number of
	// pods that are still required for the deployment to have 100% available capacity. They may
	// either be pods that are running but not yet available or pods that still have not been created.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty" protobuf:"varint,5,opt,name=unavailableReplicas"`
}

// ConsoleValues is a CRD friendly equivalent of [console.PartialValues]. Any
// member that is optional at the top level, either by being a pointer, map, or
// slice, is NOT further partial-ized. This allows us to enforce validation
// constraints without accidentally polluting the defaults of the chart.
// +hidefromdoc
type ConsoleValues struct {
	// Specifies the number of Console Pods to run.
	// Ignored when `autoscaling.enabled` is `true`, because the HorizontalPodAutoscaler then manages the replica count.
	// Defaults to `1`.
	ReplicaCount *int32 `json:"replicaCount,omitempty"`
	// Specifies the container image that runs Redpanda Console.
	Image *Image `json:"image,omitempty"`
	// Specifies Secrets that hold credentials for pulling the Console image from a private registry.
	// For details, see https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Specifies whether Kubernetes mounts a ServiceAccount token into the Console Pods.
	// Console does not call the Kubernetes API, so leave this `false` unless a sidecar in `extraContainers` needs the token.
	// Defaults to `false`.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`
	// Configures the ServiceAccount that the operator creates for the Console Pods.
	ServiceAccount *ServiceAccountConfig `json:"serviceAccount,omitempty"`
	// Adds labels to every resource that the operator creates for this Console, except the Pods.
	// To label the Pods, use `podLabels`.
	CommonLabels map[string]string `json:"commonLabels,omitempty"`
	// Adds annotations to the Console Deployment.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Adds annotations to the Console Pods.
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
	// Adds labels to the Console Pods.
	PodLabels map[string]string `json:"podLabels,omitempty"`
	// Sets the security context for the Console Pods.
	// Defaults to `runAsUser: 99`, `fsGroup: 99`, and `fsGroupChangePolicy: Always`.
	// Fields you set override the matching defaults, and the others are kept.
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// Sets the security context for the Console container.
	// Defaults to `runAsNonRoot: true`.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// Configures the Kubernetes Service that exposes Console.
	Service *ServiceConfig `json:"service,omitempty"`
	// Configures a Kubernetes Ingress that routes external traffic to the Console Service.
	Ingress *IngressConfig `json:"ingress,omitempty"`
	// Configures a Gateway API HTTPRoute that routes traffic to the Console Service.
	Gateway *GatewayConfig `json:"gateway,omitempty"`
	// Sets CPU and memory requests and limits for the Console container.
	// If unset, the container has no requests or limits.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Configures a HorizontalPodAutoscaler that scales the Console Deployment.
	Autoscaling *AutoScaling `json:"autoscaling,omitempty"`
	// Constrains the Console Pods to nodes with these labels.
	// For details, see https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Allows the Console Pods to be scheduled onto nodes with matching taints.
	// For details, see https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Sets node affinity, Pod affinity, and Pod anti-affinity rules for the Console Pods.
	// For details, see https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Controls how the Console Pods are spread across topology domains such as zones and nodes.
	// For details, see https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/.
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// Specifies the PriorityClass for the Console Pods.
	// For details, see https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass.
	PriorityClassName *string `json:"priorityClassName,omitempty"`
	// Specifies the Redpanda Console configuration, written as the contents of Console's `config.yaml`.
	// The operator stores it in a ConfigMap, mounts it at `/etc/console/configs/config.yaml`, and restarts the Pods when it changes.
	// If `cluster` is set, the operator merges the generated connection settings into this configuration.
	// For the available settings, see https://docs.redpanda.com/streaming/current/console/config/configure-console/.
	Config *runtime.RawExtension `json:"config,omitempty"`
	// Adds environment variables to the Console container, after the ones the operator sets.
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`
	// Adds environment variables to the Console container from the keys of a Secret or ConfigMap.
	ExtraEnvFrom []corev1.EnvFromSource `json:"extraEnvFrom,omitempty"`
	// Adds volumes to the Console Pods.
	// To mount them in the Console container, add matching entries to `extraVolumeMounts`.
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`
	// Mounts volumes into the Console container.
	// Each `name` must match a volume in `extraVolumes`.
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
	// Adds sidecar containers to the Console Pods, such as an authentication proxy.
	ExtraContainers []corev1.Container `json:"extraContainers,omitempty"`
	// Exposes additional ports on the Console container, such as for a second HTTP listener.
	// The Service does not expose these ports.
	ExtraContainerPorts []corev1.ContainerPort `json:"extraContainerPorts,omitempty"`
	// Mounts existing Secrets as files in the Console container.
	// For each entry, the operator adds a volume and a volume mount.
	SecretMounts []SecretMount `json:"secretMounts,omitempty"`
	// Configures the Secret that the operator creates to hold sensitive Console settings, such as passwords and TLS files.
	Secret SecretConfig `json:"secret,omitempty"`
	// References a key in an existing Secret that holds the Redpanda Enterprise license.
	// Used only when `secret.create` is `false`.
	// When `secret.create` is `true`, set the license in `secret.license` instead.
	LicenseSecretRef *corev1.SecretKeySelector `json:"licenseSecretRef,omitempty"`
	// Configures the timing of the liveness probe on the Console container.
	// The probe always sends an HTTP GET request to `/admin/health`, so only `initialDelaySeconds`, `periodSeconds`, `timeoutSeconds`, `successThreshold`, and `failureThreshold` take effect.
	// Defaults to `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, and `failureThreshold: 3`.
	LivenessProbe *ProbeApplyConfiguration `json:"livenessProbe,omitempty"`
	// Configures the timing of the readiness probe on the Console container.
	// The probe always sends an HTTP GET request to `/admin/health`, so only `initialDelaySeconds`, `periodSeconds`, `timeoutSeconds`, `successThreshold`, and `failureThreshold` take effect.
	// Defaults to `initialDelaySeconds: 10`, `periodSeconds: 10`, `timeoutSeconds: 1`, `successThreshold: 1`, and `failureThreshold: 3`.
	ReadinessProbe *ProbeApplyConfiguration `json:"readinessProbe,omitempty"`
	// Customizes the command and arguments of the Console container.
	Deployment *DeploymentConfig `json:"deployment,omitempty"`
	// Sets the strategy the Console Deployment uses to replace old Pods with new ones.
	// If unset, Kubernetes uses a rolling update.
	// For details, see https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy.
	Strategy *appsv1.DeploymentStrategy `json:"strategy,omitempty"`
	// Lists human-readable warnings generated by the automatic migration of a Console v2 configuration to a Console v3 configuration.
	// Each warning describes a field from the original configuration that was dropped, and why.
	// Setting this field has no effect.
	Warnings []string `json:"warnings,omitempty"`

	// Configures a Prometheus ServiceMonitor that scrapes Console's metrics.
	Monitoring *MonitoringConfig `json:"monitoring,omitempty"`

	// Configures Console to manage Redpanda Connect pipelines that run as Pipeline resources in the Console resource's namespace.
	// When enabled, the operator deploys its pipeline API, a Deployment whose name ends in `-pipeline-api`, and sets `config.cloud.redpandaConnect` so that Console proxies its pipeline calls there with a token of its own ServiceAccount. Console itself gets no access to the Kubernetes API.
	// Requires Console enterprise with pipeline API support.
	// Anyone who can create pipelines in Console can run code in the namespace through Connect configs, within the limits the pipeline API enforces. Enforce the Pod Security Admission `restricted` level on the namespace and set a ResourceQuota there.
	// Run one Console per namespace: every Console labels its Pipelines `console.redpanda.com/managed-by: console`, so Consoles that share a namespace can edit and delete each other's pipelines.
	Pipelines *PipelinesConfig `json:"pipelines,omitempty"`
}

// PipelinesConfig configures Console to manage Redpanda Connect pipelines.
// +kubebuilder:validation:XValidation:message="clusterRef requires userRef: set userRef to a User with ACLs scoped to Console's pipelines",rule="!has(self.enabled) || !self.enabled || !has(self.clusterRef) || has(self.userRef)"
type PipelinesConfig struct {
	// Specifies whether Console manages pipelines.
	// The operator must run with the Helm value `connectController.enabled: true`, which runs the Pipelines and grants the operator the permissions the pipeline API needs.
	// Defaults to `false`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies the name of the Redpanda resource that pipelines created in Console connect to, set as their `spec.cluster.clusterRef.name`.
	// The Redpanda resource must be in the Console resource's namespace.
	// If unset, Console creates pipelines without a cluster reference.
	// Requires `userRef`.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	ClusterRef *string `json:"clusterRef,omitempty"`
	// Specifies the name of the User resource that pipelines created in Console authenticate as, set as their `spec.userRef.name`.
	// The User must be in the Console resource's namespace and have ACLs for the topics, consumer groups, and schema subjects that the pipelines use, plus Write and Describe on the topic `__redpanda.connect.logs` for Console to show pipeline logs.
	// Every pipeline created in Console runs as this User, so its ACLs bound what anyone who can create pipelines in Console can read and write. Keep them narrow.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	UserRef *string `json:"userRef,omitempty"`
	// Sets the pipeline API container's resource requests and limits.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// AutoScaling configures a HorizontalPodAutoscaler for the Console Deployment.
type AutoScaling struct {
	// Specifies whether the operator creates a HorizontalPodAutoscaler for Console.
	// When `true`, `replicaCount` is ignored.
	// Defaults to `false`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies the minimum number of Console Pods the autoscaler keeps.
	// Defaults to `1`.
	MinReplicas *int32 `json:"minReplicas,omitempty"`
	// Specifies the maximum number of Console Pods the autoscaler can scale up to.
	// Defaults to `100`.
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`
	// Specifies the target average CPU utilization, as a percentage of the container's CPU request, at which the autoscaler adds Pods.
	// Requires a CPU request in `resources`.
	// Defaults to `80`.
	TargetCPUUtilizationPercentage *int32 `json:"targetCPUUtilizationPercentage,omitempty"`
	// Specifies the target average memory utilization, as a percentage of the container's memory request, at which the autoscaler adds Pods.
	// Requires a memory request in `resources`.
	// If unset, the autoscaler does not scale on memory.
	TargetMemoryUtilizationPercentage *int32 `json:"targetMemoryUtilizationPercentage,omitempty"`
}

// DeploymentConfig customizes the command and arguments of the Console container.
type DeploymentConfig struct {
	// Overrides the entrypoint of the Console container.
	// If unset, the image's entrypoint runs.
	Command []string `json:"command,omitempty"`
	// Adds command-line arguments to the Console container, after `--config.filepath=/etc/console/configs/config.yaml`.
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

// Image specifies the container image that runs Redpanda Console.
type Image struct {
	// Specifies the container registry that hosts the image.
	// Defaults to `docker.redpanda.com`.
	Registry *string `json:"registry,omitempty"`
	// Specifies the image repository within the registry.
	// Defaults to `redpandadata/console`.
	Repository *string `json:"repository,omitempty"`
	// Specifies when Kubernetes pulls the image: `Always`, `IfNotPresent`, or `Never`.
	// Defaults to `IfNotPresent`.
	PullPolicy *corev1.PullPolicy `json:"pullPolicy,omitempty"`
	// Specifies the image tag.
	// If unset, the operator uses the Console version that it was released with.
	Tag *string `json:"tag,omitempty"`
}

// ServiceAccountConfig configures the ServiceAccount that the operator creates for the Console Pods.
type ServiceAccountConfig struct {
	// Specifies whether the ServiceAccount allows its API token to be mounted into Pods automatically.
	// Console does not call the Kubernetes API.
	// Defaults to `false`.
	AutomountServiceAccountToken *bool `json:"automountServiceAccountToken,omitempty"`
	// Adds annotations to the ServiceAccount, such as those that bind it to a cloud identity for workload identity.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies the name of the ServiceAccount.
	// If unset, the operator derives the name from the Console resource's name.
	Name *string `json:"name,omitempty"`
}

// ServiceConfig configures the Kubernetes Service that exposes Console.
type ServiceConfig struct {
	// Specifies the Service type: `ClusterIP`, `NodePort`, or `LoadBalancer`.
	// Defaults to `ClusterIP`.
	Type *corev1.ServiceType `json:"type,omitempty"`
	// Specifies the port that the Service listens on.
	// Ingress and HTTPRoute backends also use this port.
	// Defaults to `8080`.
	Port *int32 `json:"port,omitempty"`
	// Specifies the node port to allocate.
	// Applies only when `type` is `NodePort`.
	// If unset, Kubernetes assigns a port.
	NodePort *int32 `json:"nodePort,omitempty"`
	// Specifies the container port that the Service forwards traffic to, and the port that the Console container listens on.
	// If `config.server.listenPort` is set, the container uses that port instead, so keep the two values equal.
	// If both are unset, the container listens on port 8080.
	TargetPort *int32 `json:"targetPort,omitempty"`
	// Adds annotations to the Service, such as those that configure a cloud load balancer.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// IngressConfig configures a Kubernetes Ingress for Console.
type IngressConfig struct {
	// Specifies whether the operator creates an Ingress for Console.
	// Defaults to `false`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies the IngressClass that implements the Ingress.
	// If unset, the cluster's default IngressClass is used.
	ClassName *string `json:"className,omitempty"`
	// Adds annotations to the Ingress, such as those that configure the Ingress controller or cert-manager.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies the hostnames and paths that the Ingress routes to the Console Service.
	// If unset, the Ingress routes the path `/` on the placeholder host `chart-example.local`, so set this when you enable the Ingress.
	Hosts []IngressHost `json:"hosts,omitempty"`
	// Configures TLS termination for the Ingress.
	// Each entry names a Secret that holds a TLS certificate and the hosts that the certificate covers.
	TLS []networkingv1.IngressTLS `json:"tls,omitempty"`
}

// IngressHost is a hostname and its paths in the Console Ingress.
type IngressHost struct {
	// Specifies the fully qualified domain name that the Ingress serves Console on.
	Host string `json:"host,omitempty"`
	// Specifies the paths on this host that route to the Console Service.
	Paths []IngressPath `json:"paths,omitempty"`
}

// IngressPath is a path in the Console Ingress.
type IngressPath struct {
	// Specifies the URL path that routes to the Console Service, such as `/`.
	Path string `json:"path,omitempty"`
	// Specifies how the Ingress matches `path`: `Exact`, `Prefix`, or `ImplementationSpecific`.
	// Kubernetes requires a value.
	PathType *networkingv1.PathType `json:"pathType,omitempty"`
}

// GatewayConfig configures a Gateway API HTTPRoute for Console.
type GatewayConfig struct {
	// Specifies whether the operator creates an HTTPRoute for Console.
	// Requires the Gateway API CRDs in the cluster.
	// Defaults to `false`.
	Enabled *bool `json:"enabled,omitempty"`
	// Adds annotations to the HTTPRoute.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies the Gateways that the HTTPRoute attaches to.
	// If empty, the HTTPRoute is created but does not attach to any Gateway.
	ParentRefs []GatewayParentReference `json:"parentRefs,omitempty"`
	// Specifies the hostnames that the HTTPRoute matches.
	// Defaults to the placeholder `chart-example.local`, so set this when you enable the HTTPRoute.
	Hostnames []string `json:"hostnames,omitempty"`
	// Specifies the URL path that routes to the Console Service.
	// Defaults to `/`.
	Path *string `json:"path,omitempty"`
	// Specifies how the HTTPRoute matches `path`: `Exact`, `PathPrefix`, or `RegularExpression`.
	// Defaults to `PathPrefix`.
	PathType *gatewayv1.PathMatchType `json:"pathType,omitempty"`
}

// GatewayParentReference identifies a parent Gateway for the HTTPRoute.
type GatewayParentReference struct {
	// Specifies the name of the Gateway.
	Name string `json:"name"`
	// Specifies the namespace of the Gateway.
	// If unset, the Gateway must be in the same namespace as the Console resource.
	Namespace *string `json:"namespace,omitempty"`
	// Specifies the name of the Gateway listener to attach to.
	// If unset, the HTTPRoute attaches to every listener on the Gateway that allows it.
	SectionName *gatewayv1.SectionName `json:"sectionName,omitempty"`
}

// SecretMount mounts an existing Secret as files in the Console container.
type SecretMount struct {
	// Specifies the name of the volume that the operator creates for the Secret.
	// Must be unique among the Pod's volumes.
	Name string `json:"name,omitempty"`
	// Specifies the name of the Secret to mount.
	// The Secret must be in the same namespace as the Console resource.
	SecretName string `json:"secretName,omitempty"`
	// Specifies the directory in the Console container where the Secret's keys appear as files.
	Path string `json:"path,omitempty"`
	// Mounts a single key of the Secret at `path` instead of the whole Secret.
	SubPath *string `json:"subPath,omitempty"`
	// Specifies the permission bits of the mounted files.
	// If unset, Kubernetes uses `0644`.
	DefaultMode *int32 `json:"defaultMode,omitempty"`
}

// SecretConfig configures the Secret that the operator creates to hold sensitive Console settings.
// The operator mounts the Secret at `/etc/console/secrets` and passes each value that you set to Console, either as an environment variable or as the path to a file.
type SecretConfig struct {
	// Specifies whether the operator creates the Secret.
	// When `false`, the operator passes none of the values in `secret` to Console, including the JWT signing key that it generates, and reads the license from `licenseSecretRef`.
	// Provide the settings through `extraEnv`, `extraEnvFrom`, or `secretMounts` instead.
	// Defaults to `true`.
	Create *bool `json:"create,omitempty"`
	// Specifies credentials and TLS files for the Kafka API.
	Kafka *KafkaSecrets `json:"kafka,omitempty"`
	// Specifies secrets for Console's login authentication.
	Authentication *AuthenticationSecrets `json:"authentication,omitempty"`
	// Specifies the Redpanda Enterprise license.
	// The operator passes it to Console in the `LICENSE` environment variable.
	// To read the license from an existing Secret instead, set `create` to `false` and use `licenseSecretRef`.
	License *string `json:"license,omitempty"`
	// Specifies credentials and TLS files for the Redpanda Admin API.
	Redpanda *RedpandaSecrets `json:"redpanda,omitempty"`
	// Specifies credentials for deserializing messages.
	Serde *SerdeSecrets `json:"serde,omitempty"`
	// Specifies credentials and TLS files for the Schema Registry.
	SchemaRegistry *SchemaRegistrySecrets `json:"schemaRegistry,omitempty"`
}

// KafkaSecrets holds the credentials and TLS files that Console uses to connect to the Kafka API.
type KafkaSecrets struct {
	// Specifies the SASL password.
	// The operator passes it to Console in the `KAFKA_SASL_PASSWORD` environment variable.
	SASLPassword *string `json:"saslPassword,omitempty"`
	// Specifies the AWS secret access key for AWS MSK IAM authentication.
	// The operator passes it to Console in the `KAFKA_SASL_AWSMSKIAM_SECRETKEY` environment variable.
	AWSMSKIAMSecretKey *string `json:"awsMskIamSecretKey,omitempty"`
	// Specifies the PEM-encoded CA certificate that Console uses to verify the Kafka API's TLS certificate.
	// The operator mounts it as a file and sets `KAFKA_TLS_CAFILEPATH` to its path.
	TLSCA *string `json:"tlsCa,omitempty"`
	// Specifies the PEM-encoded client certificate that Console presents for mTLS.
	// The operator mounts it as a file and sets `KAFKA_TLS_CERTFILEPATH` to its path.
	TLSCert *string `json:"tlsCert,omitempty"`
	// Specifies the PEM-encoded private key for `tlsCert`.
	// The operator mounts it as a file and sets `KAFKA_TLS_KEYFILEPATH` to its path.
	TLSKey *string `json:"tlsKey,omitempty"`
	// Has no effect.
	// The operator does not pass this value to Console.
	TLSPassphrase *string `json:"tlsPassphrase,omitempty"`
}

// SchemaRegistrySecrets holds the credentials and TLS files that Console uses to connect to the Schema Registry.
type SchemaRegistrySecrets struct {
	// Specifies the bearer token for Schema Registry authentication.
	// The operator passes it to Console in the `SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN` environment variable.
	BearerToken *string `json:"bearerToken,omitempty"`
	// Specifies the password for Schema Registry basic authentication.
	// The operator passes it to Console in the `SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD` environment variable.
	Password *string `json:"password,omitempty"`
	// Specifies the PEM-encoded CA certificate that Console uses to verify the Schema Registry's TLS certificate.
	// The operator mounts it as a file and sets `SCHEMAREGISTRY_TLS_CAFILEPATH` to its path.
	TLSCA *string `json:"tlsCa,omitempty"`
	// Specifies the PEM-encoded client certificate that Console presents to the Schema Registry.
	// The operator mounts it as a file and sets `SCHEMAREGISTRY_TLS_CERTFILEPATH` to its path.
	TLSCert *string `json:"tlsCert,omitempty"`
	// Specifies the PEM-encoded private key for `tlsCert`.
	// The operator mounts it as a file and sets `SCHEMAREGISTRY_TLS_KEYFILEPATH` to its path.
	TLSKey *string `json:"tlsKey,omitempty"`
}

// AuthenticationSecrets holds the secrets for Console's login authentication.
type AuthenticationSecrets struct {
	// Specifies the key that Console uses to sign the JSON Web Tokens for login sessions.
	// Must be at least 32 characters.
	// The operator passes it to Console in the `AUTHENTICATION_JWTSIGNINGKEY` environment variable.
	// If unset, the operator generates a random key and stores it in an immutable Secret named `<console-name>-jwt-secret`, so sessions remain valid when Pods restart.
	JWTSigningKey *string `json:"jwtSigningKey,omitempty"`
	// Specifies secrets for OIDC login.
	OIDC *OIDCLoginSecrets `json:"oidc,omitempty"`
}

// OIDCLoginSecrets holds the secrets for Console's OIDC login.
type OIDCLoginSecrets struct {
	// Specifies the OIDC client secret.
	// The operator stores it in its Secret under the key `authentication-oidc-client-secret`, but does not pass it to Console.
	// To use it, add an `extraEnv` entry named `AUTHENTICATION_OIDC_CLIENTSECRET` that references this key, or set `authentication.oidc.clientSecret` in `config`.
	ClientSecret *string `json:"clientSecret,omitempty"`
}

// RedpandaSecrets holds the secrets that Console uses to connect to Redpanda's own APIs.
type RedpandaSecrets struct {
	// Specifies credentials and TLS files for the Redpanda Admin API.
	AdminAPI *RedpandaAdminAPISecrets `json:"adminApi,omitempty"`
}

// SerdeSecrets holds the secrets that Console uses to deserialize messages.
type SerdeSecrets struct {
	// Specifies the basic authentication password for the Git repository that holds Protobuf schemas.
	// The operator passes it to Console in the `SERDE_PROTOBUF_GIT_BASICAUTH_PASSWORD` environment variable.
	ProtobufGitBasicAuthPassword *string `json:"protobufGitBasicAuthPassword,omitempty"`
}

// RedpandaAdminAPISecrets holds the credentials and TLS files that Console uses to connect to the Redpanda Admin API.
type RedpandaAdminAPISecrets struct {
	// Specifies the password for Admin API basic authentication.
	// The operator passes it to Console in the `REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD` environment variable.
	// Set the username in `config` at `redpanda.adminApi.authentication.basic.username`.
	Password *string `json:"password,omitempty"`
	// Specifies the PEM-encoded CA certificate that Console uses to verify the Admin API's TLS certificate.
	// The operator mounts it as a file and sets `REDPANDA_ADMINAPI_TLS_CAFILEPATH` to its path.
	TLSCA *string `json:"tlsCa,omitempty"`
	// Specifies the PEM-encoded client certificate that Console presents to the Admin API.
	// The operator mounts it as a file and sets `REDPANDA_ADMINAPI_TLS_CERTFILEPATH` to its path.
	TLSCert *string `json:"tlsCert,omitempty"`
	// Specifies the PEM-encoded private key for `tlsCert`.
	// The operator mounts it as a file and sets `REDPANDA_ADMINAPI_TLS_KEYFILEPATH` to its path.
	TLSKey *string `json:"tlsKey,omitempty"`
}

// ProbeApplyConfiguration is a wrapper type that allows including a partial
// [corev1.Probe] in a CRD.
type ProbeApplyConfiguration struct {
	*applycorev1.ProbeApplyConfiguration `json:",inline"`
}

func (ac *ProbeApplyConfiguration) DeepCopy() *ProbeApplyConfiguration {
	// For some inexplicable reason, apply configs don't have deepcopy
	// generated for them.
	//
	// DeepCopyInto can be generated with just DeepCopy implemented. Sadly, the
	// easiest way to implement DeepCopy is to run this type through JSON. It's
	// highly unlikely that we'll hit a panic but it is possible to do so with
	// invalid values for resource.Quantity and the like.
	out := new(ProbeApplyConfiguration)
	data, err := json.Marshal(ac)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}

// ConvertConsoleSubchartToConsoleValues "migrates" the Console field
// ([RedpandaConsole]) of a [Redpanda] into a Console v3 compliant
// [ConsoleValues].
func ConvertConsoleSubchartToConsoleValues(src *RedpandaConsole) (*ConsoleValues, error) {
	// By the redpanda chart's default values, console is enabled by default
	// and must be explicitly opted out of.
	if src == nil {
		// Empty values is valid.
		return &ConsoleValues{}, nil
	}

	// If the console integration is opted out of, return nil.
	if !ptr.Deref(src.Enabled, true) {
		return nil, nil
	}

	out, err := autoconv_RedpandaConsole_To_ConsoleValues(src)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Extract out .Console and .Config. .Console will be migrated and then
	// merged into .Config as Config is meant to house V3 configurations.
	var v2Config map[string]any
	if src.DeprecatedConsole != nil && len(src.DeprecatedConsole.Raw) > 0 {
		if err := json.Unmarshal(src.DeprecatedConsole.Raw, &v2Config); err != nil {
			return nil, errors.WithStack(err)
		}
	}

	var v3Config map[string]any
	if src.Config != nil && len(src.Config.Raw) > 0 {
		if err := json.Unmarshal(src.Config.Raw, &v3Config); err != nil {
			return nil, errors.WithStack(err)
		}
	}

	migrated, warnings, err := console.ConfigFromV2(v2Config)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	merged := functional.MergeMaps(migrated, v3Config)

	marshalled, err := json.Marshal(merged)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	out.Config = &runtime.RawExtension{Raw: marshalled}
	// Unlike the docs migrate, warnings get their own field. We can't set
	// comments of a Kubernetes resource.
	out.Warnings = warnings

	return out, nil
}

// MonitoringConfig configures a Prometheus ServiceMonitor for Console.
type MonitoringConfig struct {
	// Specifies whether the operator creates a ServiceMonitor that scrapes Console's `/admin/metrics` endpoint.
	// Requires the Prometheus Operator CRDs in the cluster.
	// Defaults to `false`.
	Enabled *bool `json:"enabled,omitempty"`
	// Specifies how often Prometheus scrapes Console, as a Prometheus duration such as `30s` or `1m`.
	// Defaults to `1m`.
	ScrapeInterval *string `json:"scrapeInterval,omitempty"`
	// Adds labels to the ServiceMonitor, such as the labels that your Prometheus instance uses to select ServiceMonitors.
	Labels map[string]string `json:"labels"`
}
