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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// RedpandaBrokerPool defines a pool of Redpanda brokers, deployed as one
// StatefulSet, for the Redpanda or StretchCluster resource that `clusterRef`
// references. Managing RedpandaBrokerPools requires an enterprise license.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandabrokerpools
// +kubebuilder:resource:shortName=rpbrokerpool
// +kubebuilder:printcolumn:name="Bound",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].status",description=""
// +kubebuilder:printcolumn:name="Deployed",type="string",JSONPath=".status.conditions[?(@.type==\"Deployed\")].status",description=""
type RedpandaBrokerPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BrokerPoolSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Bound", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Deployed", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status BrokerPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RedpandaBrokerPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RedpandaBrokerPool `json:"items"`
}

func (s *RedpandaBrokerPoolList) GetItems() []*RedpandaBrokerPool {
	return functional.MapFn(ptr.To, s.Items)
}

// GetClusterSource implements [ClusterReferencingObject].
func (n *RedpandaBrokerPool) GetClusterSource() *ClusterSource {
	return &ClusterSource{
		ClusterRef: &n.Spec.ClusterRef,
	}
}

// BrokerPoolSpec defines the desired state of a broker pool. For a pool of a
// Redpanda resource, unset fields take the defaults of the Redpanda Helm
// chart, as a NodePool's do; for a pool of a StretchCluster, unset fields
// inherit the StretchCluster's values.
// +kubebuilder:validation:XValidation:message="clusterDomain, tls, external, listeners, rbac, serviceAccount, monitoring, storage, resources, imagePullSecrets, rackAwareness, and logging are only supported when clusterRef references a StretchCluster",rule="(has(self.clusterRef.kind) && self.clusterRef.kind == 'StretchCluster') || !(has(self.clusterDomain) || has(self.tls) || has(self.external) || has(self.listeners) || has(self.rbac) || has(self.serviceAccount) || has(self.monitoring) || has(self.storage) || has(self.resources) || has(self.imagePullSecrets) || has(self.rackAwareness) || has(self.logging))"
type BrokerPoolSpec struct {
	EmbeddedBrokerPoolSpec `json:",inline"`
	// Specifies the Redpanda or StretchCluster resource this pool belongs
	// to. `kind` defaults to `Redpanda`; set it to `StretchCluster` for a
	// pool of a stretch cluster.
	ClusterRef ClusterRef `json:"clusterRef"`
}

type EmbeddedBrokerPoolSpec struct {
	// Adds labels to the pod selector and pod template of this pool's
	// StatefulSet, so you can group pods by your own dimensions, such as team
	// or environment.
	AdditionalSelectorLabels map[string]string `json:"additionalSelectorLabels,omitempty"`
	// Specifies the number of brokers in this pool. Defaults to 3 for a pool
	// of a Redpanda resource and to 1 for a pool of a StretchCluster.
	Replicas *int32 `json:"replicas,omitempty"`
	// Appends flags to the `rpk redpanda start` command of every broker in
	// this pool.
	AdditionalRedpandaCmdFlags []string `json:"additionalRedpandaCmdFlags,omitempty"`
	// Defines a strategic merge patch that the operator applies on top of the
	// pod template it renders for this pool, for example to add node
	// selectors, tolerations, volumes, sidecar containers, or security
	// contexts.
	PodTemplate *PodTemplate `json:"podTemplate,omitempty"`
	// Overrides the per-pod Services that the operator creates for this
	// pool's brokers, such as their selectors and annotations, or whether
	// Services are created for brokers in other Kubernetes clusters. Takes
	// effect only for a pool of a StretchCluster.
	Services *NodePoolServices `json:"services,omitempty"`
	// Configures the init containers the operator renders for this pool's
	// brokers: the filesystem validator, the data directory ownership fix,
	// and the configurator.
	InitContainers *PoolInitContainers `json:"initContainers,omitempty"`
	// Specifies the Redpanda container image for this pool's brokers. For a
	// pool of a Redpanda resource, each unset field inherits from `image` on
	// that resource, which in turn defaults to the operator's Redpanda
	// repository and version; set `tag` only to run this pool on a different
	// Redpanda version than the rest of the cluster. For a pool of a
	// StretchCluster, unset fields default to the operator's Redpanda
	// repository and version.
	Image *RedpandaImage `json:"image,omitempty"`
	// Specifies the image of the operator sidecar container in this pool's
	// broker pods. Defaults to the operator's own image.
	SidecarImage *RedpandaImage `json:"sidecarImage,omitempty"`
	// Specifies the image for init containers that do not need the Redpanda
	// binary, such as the data directory ownership fix. Defaults to
	// `busybox:latest`.
	InitContainerImage *InitContainerImage `json:"initContainerImage,omitempty"`
	// Overrides the lifecycle policy for the PersistentVolumeClaims of this
	// pool's StatefulSet. When set, it replaces the cluster-level
	// `statefulset.persistentVolumeClaimRetentionPolicy`. When unset, the
	// cluster-level value applies, or the Kubernetes default of `Retain` for
	// both `whenScaled` and `whenDeleted`. Set `whenScaled: Delete` to delete a
	// broker's PersistentVolumeClaim when a scale-down decommissions it, and
	// `whenDeleted: Delete` to delete every PersistentVolumeClaim when the
	// pool's StatefulSet is deleted.
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// Specifies the Kubernetes cluster DNS domain used in the internal
	// hostnames of this pool's brokers (`<pod>.<service>.<namespace>.svc.<domain>`).
	// Defaults to `cluster.local`. Supported only for a pool of a
	// StretchCluster.
	ClusterDomain *string `json:"clusterDomain,omitempty"`
	// Configures the TLS certificates of this pool's listeners: server and
	// client certificates, CA settings, and custom issuers. Pools share the
	// cluster-wide root CA that the operator manages. Supported only for a
	// pool of a StretchCluster.
	TLS *TLS `json:"tls,omitempty"`
	// Configures external access to this pool's brokers: NodePort or
	// LoadBalancer Services, advertised addresses, and ExternalDNS. Supported
	// only for a pool of a StretchCluster.
	External *External `json:"external,omitempty"`
	// Configures the Admin, Kafka, HTTP Proxy, Schema Registry, and RPC
	// listeners of this pool's brokers: ports, TLS certificate references,
	// and external listeners. Supported only for a pool of a StretchCluster.
	Listeners *StretchListeners `json:"listeners,omitempty"`
	// Configures the Roles and ClusterRoles that the operator manages for
	// this pool's ServiceAccount, such as sidecar, `rpk debug bundle`, and
	// metrics-reader permissions. Supported only for a pool of a
	// StretchCluster.
	RBAC *RBAC `json:"rbac,omitempty"`
	// Configures the ServiceAccount of this pool's broker pods: whether the
	// operator creates it and its name, so you can bring a pre-created
	// ServiceAccount with your own IAM bindings. Supported only for a pool of
	// a StretchCluster.
	ServiceAccount *ServiceAccount `json:"serviceAccount,omitempty"`
	// Enables a Prometheus ServiceMonitor for this pool. Supported only for a
	// pool of a StretchCluster.
	Monitoring *Monitoring `json:"monitoring,omitempty"`
	// Configures the Redpanda data directory and the Tiered Storage cache of
	// this pool's brokers: hostPath, PersistentVolumeClaim, or emptyDir. Each
	// field you set overrides the StretchCluster's `storage`; unset fields
	// inherit it. Supported only for a pool of a StretchCluster.
	Storage *StretchStorage `json:"storage,omitempty"`
	// Configures the resource requests and limits of this pool's Redpanda
	// containers. Each field you set overrides the StretchCluster's
	// `resources`, key by key for `limits` and `requests`; unset fields
	// inherit it. Supported only for a pool of a StretchCluster.
	Resources *StretchResources `json:"resources,omitempty"`
	// Lists the Secrets used to pull the Redpanda, sidecar, and init container
	// images from private registries. A non-empty list replaces the
	// StretchCluster's `imagePullSecrets`; an empty list inherits it.
	// Supported only for a pool of a StretchCluster.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// Configures the Kubernetes node label that the operator maps to each
	// broker's `rack`, so Redpanda spreads replicas across racks or zones.
	// Supported only for a pool of a StretchCluster.
	RackAwareness *RackAwareness `json:"rackAwareness,omitempty"`
	// Overrides the log level and usage statistics settings of this pool's
	// brokers. Supported only for a pool of a StretchCluster.
	Logging *StretchLogging `json:"logging,omitempty"`
}

// BrokerPoolStatus defines the observed state of a broker pool.
type BrokerPoolStatus struct {
	EmbeddedBrokerPoolStatus `json:",inline"`
	// Reports the generation of this RedpandaBrokerPool that the operator
	// has deployed.
	DeployedGeneration int64 `json:"deployedGeneration,omitempty"`
	// Reports the conditions of this broker pool.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EmbeddedBrokerPoolStatus defines the observed state of a broker pool's
// brokers.
type EmbeddedBrokerPoolStatus struct {
	// Reports the name of the pool.
	Name string `json:"name,omitempty"`
	// Reports the number of brokers that currently exist in the pool. It
	// differs from `desiredReplicas` while the pool scales.
	Replicas int32 `json:"replicas,omitempty"`
	// Reports the number of brokers the pool should run.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// Reports the number of brokers that do not yet match the pool's
	// definition. The operator rolls these brokers.
	OutOfDateReplicas int32 `json:"outOfDateReplicas,omitempty"`
	// Reports the number of brokers that match the pool's definition.
	UpToDateReplicas int32 `json:"upToDateReplicas,omitempty"`
	// Reports the number of brokers that a scale-down will decommission.
	CondemnedReplicas int32 `json:"condemnedReplicas,omitempty"`
	// Reports the number of brokers whose readiness probes pass.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// Reports the number of brokers that are running.
	RunningReplicas int32 `json:"runningReplicas,omitempty"`
}
