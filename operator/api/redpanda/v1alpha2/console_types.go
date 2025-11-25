// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

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

	Spec   ConsoleSpec   `json:"spec,omitempty"`
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

type ConsoleSpec struct {
	ConsoleValues `json:",inline"`
	ClusterSource *ClusterSource `json:"cluster,omitempty"`
}

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
	ReplicaCount                 *int32                            `json:"replicaCount,omitempty"`
	Image                        *Image                            `json:"image,omitempty"`
	ImagePullSecrets             []corev1.LocalObjectReference     `json:"imagePullSecrets,omitempty"`
	AutomountServiceAccountToken *bool                             `json:"automountServiceAccountToken,omitempty"`
	ServiceAccount               *ServiceAccountConfig             `json:"serviceAccount,omitempty"`
	CommonLabels                 map[string]string                 `json:"commonLabels,omitempty"`
	Annotations                  map[string]string                 `json:"annotations,omitempty"`
	PodAnnotations               map[string]string                 `json:"podAnnotations,omitempty"`
	PodLabels                    map[string]string                 `json:"podLabels,omitempty"`
	PodSecurityContext           *corev1.PodSecurityContext        `json:"podSecurityContext,omitempty"`
	SecurityContext              *corev1.SecurityContext           `json:"securityContext,omitempty"`
	Service                      *ServiceConfig                    `json:"service,omitempty"`
	Ingress                      *IngressConfig                    `json:"ingress,omitempty"`
	Resources                    *corev1.ResourceRequirements      `json:"resources,omitempty"`
	Autoscaling                  *AutoScaling                      `json:"autoscaling,omitempty"`
	NodeSelector                 map[string]string                 `json:"nodeSelector,omitempty"`
	Tolerations                  []corev1.Toleration               `json:"tolerations,omitempty"`
	Affinity                     *corev1.Affinity                  `json:"affinity,omitempty"`
	TopologySpreadConstraints    []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	PriorityClassName            *string                           `json:"priorityClassName,omitempty"`
	Config                       *runtime.RawExtension             `json:"config,omitempty"`
	ExtraEnv                     []corev1.EnvVar                   `json:"extraEnv,omitempty"`
	ExtraEnvFrom                 []corev1.EnvFromSource            `json:"extraEnvFrom,omitempty"`
	ExtraVolumes                 []corev1.Volume                   `json:"extraVolumes,omitempty"`
	ExtraVolumeMounts            []corev1.VolumeMount              `json:"extraVolumeMounts,omitempty"`
	ExtraContainers              []corev1.Container                `json:"extraContainers,omitempty"`
	ExtraContainerPorts          []corev1.ContainerPort            `json:"extraContainerPorts,omitempty"`
	SecretMounts                 []SecretMount                     `json:"secretMounts,omitempty"`
	Secret                       SecretConfig                      `json:"secret,omitempty"`
	LicenseSecretRef             *corev1.SecretKeySelector         `json:"licenseSecretRef,omitempty"`
	LivenessProbe                *corev1.Probe                     `json:"livenessProbe,omitempty"`
	ReadinessProbe               *corev1.Probe                     `json:"readinessProbe,omitempty"`
	Deployment                   *DeploymentConfig                 `json:"deployment,omitempty"`
	Strategy                     *appsv1.DeploymentStrategy        `json:"strategy,omitempty"`
}

type AutoScaling struct {
	Enabled                           *bool  `json:"enabled,omitempty"`
	MinReplicas                       *int32 `json:"minReplicas,omitempty"`
	MaxReplicas                       *int32 `json:"maxReplicas,omitempty"`
	TargetCPUUtilizationPercentage    *int32 `json:"targetCPUUtilizationPercentage,omitempty"`
	TargetMemoryUtilizationPercentage *int32 `json:"targetMemoryUtilizationPercentage,omitempty"`
}

type DeploymentConfig struct {
	Command   []string `json:"command,omitempty"`
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

type Image struct {
	Registry   *string            `json:"registry,omitempty"`
	Repository *string            `json:"repository,omitempty"`
	PullPolicy *corev1.PullPolicy `json:"pullPolicy,omitempty"`
	Tag        *string            `json:"tag,omitempty"`
}

type ServiceAccountConfig struct {
	AutomountServiceAccountToken *bool             `json:"automountServiceAccountToken,omitempty"`
	Annotations                  map[string]string `json:"annotations,omitempty"`
	Name                         *string           `json:"name,omitempty"`
}

type ServiceConfig struct {
	Type        *corev1.ServiceType `json:"type,omitempty"`
	Port        *int32              `json:"port,omitempty"`
	NodePort    *int32              `json:"nodePort,omitempty"`
	TargetPort  *int32              `json:"targetPort,omitempty"`
	Annotations map[string]string   `json:"annotations,omitempty"`
}

type IngressConfig struct {
	Enabled     *bool                     `json:"enabled,omitempty"`
	ClassName   *string                   `json:"className,omitempty"`
	Annotations map[string]string         `json:"annotations,omitempty"`
	Hosts       []IngressHost             `json:"hosts,omitempty"`
	TLS         []networkingv1.IngressTLS `json:"tls,omitempty"`
}

type IngressHost struct {
	Host  string        `json:"host,omitempty"`
	Paths []IngressPath `json:"paths,omitempty"`
}

type IngressPath struct {
	Path     string                 `json:"path,omitempty"`
	PathType *networkingv1.PathType `json:"pathType,omitempty"`
}

type SecretMount struct {
	Name        string  `json:"name,omitempty"`
	SecretName  string  `json:"secretName,omitempty"`
	Path        string  `json:"path,omitempty"`
	SubPath     *string `json:"subPath,omitempty"`
	DefaultMode *int32  `json:"defaultMode,omitempty"`
}

type SecretConfig struct {
	Create         *bool                  `json:"create,omitempty"`
	Kafka          *KafkaSecrets          `json:"kafka,omitempty"`
	Authentication *AuthenticationSecrets `json:"authentication,omitempty"`
	License        *string                `json:"license,omitempty"`
	Redpanda       *RedpandaSecrets       `json:"redpanda,omitempty"`
	Serde          *SerdeSecrets          `json:"serde,omitempty"`
	SchemaRegistry *SchemaRegistrySecrets `json:"schemaRegistry,omitempty"`
}

type KafkaSecrets struct {
	SASLPassword       *string `json:"saslPassword,omitempty"`
	AWSMSKIAMSecretKey *string `json:"awsMskIamSecretKey,omitempty"`
	TLSCA              *string `json:"tlsCa,omitempty"`
	TLSCert            *string `json:"tlsCert,omitempty"`
	TLSKey             *string `json:"tlsKey,omitempty"`
	TLSPassphrase      *string `json:"tlsPassphrase,omitempty"`
}

type SchemaRegistrySecrets struct {
	BearerToken *string `json:"bearerToken,omitempty"`
	Password    *string `json:"password,omitempty"`
	TLSCA       *string `json:"tlsCa,omitempty"`
	TLSCert     *string `json:"tlsCert,omitempty"`
	TLSKey      *string `json:"tlsKey,omitempty"`
}

type AuthenticationSecrets struct {
	JWTSigningKey *string           `json:"jwtSigningKey,omitempty"`
	OIDC          *OIDCLoginSecrets `json:"oidc,omitempty"`
}

type OIDCLoginSecrets struct {
	ClientSecret *string `json:"clientSecret,omitempty"`
}

type RedpandaSecrets struct {
	AdminAPI *RedpandaAdminAPISecrets `json:"adminApi,omitempty"`
}

type SerdeSecrets struct {
	ProtobufGitBasicAuthPassword *string `json:"protobufGitBasicAuthPassword,omitempty"`
}

type RedpandaAdminAPISecrets struct {
	Password *string `json:"password,omitempty"`
	TLSCA    *string `json:"tlsCa,omitempty"`
	TLSCert  *string `json:"tlsCert,omitempty"`
	TLSKey   *string `json:"tlsKey,omitempty"`
}
