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

// NodePoolSpec contains the node pool spec for the given node pool.
// Note that the defaulting behavior comes from the underlying Redpanda
// chart renderer, the attributes specified here will get merged in and
// override the defaults.
type BrokerPoolSpec struct {
	EmbeddedBrokerPoolSpec `json:",inline"`
	ClusterRef             ClusterRef `json:"clusterRef"`
}

type EmbeddedBrokerPoolSpec struct {
	// AdditionalSelectorLabels are extra labels added to the StatefulSet's
	// pod selector and pod template. Lets operators bucket pods by custom
	// dimensions (e.g. team, environment) without affecting the operator-
	// managed labels.
	AdditionalSelectorLabels map[string]string `json:"additionalSelectorLabels,omitempty"`
	// Replicas is the desired number of broker pods in this pool. Defaults
	// to 1 if unset.
	Replicas *int32 `json:"replicas,omitempty"`
	// AdditionalRedpandaCmdFlags are extra flags appended to the
	// `rpk redpanda start` command line for every broker in this pool.
	AdditionalRedpandaCmdFlags []string `json:"additionalRedpandaCmdFlags,omitempty"`
	// PodTemplate is a strategic-merge patch applied on top of the
	// operator-rendered Pod template. Common uses: nodeSelector,
	// tolerations, custom volumes, sidecar containers, security context
	// overrides.
	PodTemplate *PodTemplate `json:"podTemplate,omitempty"`
	// Services configures overrides for the per-pod Services created by
	// the operator (e.g. selector/annotation tweaks, remote-Service
	// suppression).
	Services *NodePoolServices `json:"services,omitempty"`
	// InitContainers controls the init containers the operator renders
	// (fs-validator, set-data-dir-ownership, configurator). Lets users
	// toggle them on/off and tune flags.
	InitContainers *PoolInitContainers `json:"initContainers,omitempty"`
	// Image is the Redpanda container image (repository + tag) for this
	// pool. Defaults to the operator's built-in chart-pinned image when
	// unset.
	Image *RedpandaImage `json:"image,omitempty"`
	// SidecarImage is the operator sidecar container image for this pool.
	// Defaults to the operator's own image at build time.
	SidecarImage *RedpandaImage `json:"sidecarImage,omitempty"`
	// InitContainerImage is the image used for init containers that don't
	// need the full Redpanda binary (e.g. busybox for fs-validator,
	// chown). Defaults to busybox:latest.
	InitContainerImage *InitContainerImage `json:"initContainerImage,omitempty"`
	// PersistentVolumeClaimRetentionPolicy overrides the lifecycle policy for
	// PersistentVolumeClaims on this NodePool's StatefulSet. When set, it replaces
	// any value inherited from the parent Redpanda CRD's
	// `statefulset.persistentVolumeClaimRetentionPolicy`. When unset, the cluster-level
	// value (or the Kubernetes default of `Retain`/`Retain` if also unset) is used.
	// Set `whenScaled: Delete` to delete a broker's PVC when it is decommissioned via
	// scale-down, and `whenDeleted: Delete` to delete all PVCs when the NodePool's
	// StatefulSet is deleted.
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`
	// ClusterDomain customizes the Kubernetes cluster DNS domain used to
	// generate internal pod hostnames (`<pod>.<svc>.<ns>.svc.<domain>`).
	// Defaults to `cluster.local`. Per-pool to support clusters that span
	// k8s environments with different DNS roots.
	ClusterDomain *string `json:"clusterDomain,omitempty"`
	// TLS configures the listener cert set (server certs, optional client
	// certs, CA toggles, custom issuers). Per-pool so different pools can
	// run different listener TLS configurations while sharing the
	// cluster-wide root CA managed by the operator.
	TLS *TLS `json:"tls,omitempty"`
	// External configures external access (NodePort / LoadBalancer types,
	// advertised addresses, ExternalDNS hookup). Per-pool so pools in
	// different regions/clouds can expose themselves differently.
	External *External `json:"external,omitempty"`
	// Listeners configures the Admin / Kafka / HTTP-Proxy / Schema-Registry
	// / RPC listeners (ports, TLS cert references, external listener
	// blocks). Per-pool to permit heterogeneous listener layouts.
	Listeners *StretchListeners `json:"listeners,omitempty"`
	// RBAC toggles operator-managed Roles / ClusterRoles for this pool
	// (sidecar perms, rpk-debug-bundle perms, metrics-reader). Per-pool
	// because the bound ServiceAccount is per-pool.
	RBAC *RBAC `json:"rbac,omitempty"`
	// ServiceAccount controls creation and naming of the per-pool
	// ServiceAccount used by broker / sidecar pods. Per-pool so users can
	// pre-create SAs with their own IAM bindings (workload identity, etc.).
	ServiceAccount *ServiceAccount `json:"serviceAccount,omitempty"`
	// Monitoring toggles the per-pool ServiceMonitor. Per-pool so different
	// pools can opt in/out of Prometheus scraping independently.
	Monitoring *Monitoring `json:"monitoring,omitempty"`
	// Storage configures the Redpanda data directory and tiered-storage
	// cache (hostPath / PVC / emptyDir). When also set on
	// StretchClusterSpec, the pool's value wins on a deep field-by-field
	// merge — pool's non-nil subfields override, cluster fills any
	// subfields the pool didn't set. See BrokerPoolSpec.MergeFromCluster.
	Storage *StretchStorage `json:"storage,omitempty"`
	// Resources configures container resource requests/limits (and the
	// legacy CPU/Memory shorthand). When also set on StretchClusterSpec,
	// the pool's value wins on a deep field-by-field merge, with
	// per-key merging on the Limits/Requests maps. See
	// BrokerPoolSpec.MergeFromCluster.
	Resources *StretchResources `json:"resources,omitempty"`
	// ImagePullSecrets are private-registry credentials for pulling the
	// Redpanda / sidecar / init-container images. When non-empty, this
	// list fully overrides any list set on StretchClusterSpec; when
	// empty/nil, the cluster's list is inherited. See
	// BrokerPoolSpec.MergeFromCluster.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// RackAwareness configures the Kubernetes node-label that's mapped to
	// the broker's `rack` config so Redpanda places replicas across racks/
	// zones. Per-pool because rack labels can differ between K8s clusters.
	RackAwareness *RackAwareness `json:"rackAwareness,omitempty"`
	// Logging overrides the log level and usage-stats settings for brokers
	// in this pool. Per-pool so a noisy debug pool doesn't force the
	// whole cluster into verbose logging.
	Logging *StretchLogging `json:"logging,omitempty"`
}

// BrokerPoolStatus defines the observed state of any node pools tied to this cluster
type BrokerPoolStatus struct {
	EmbeddedBrokerPoolStatus `json:",inline"`
	// DeployedGeneration represents the generation of the NodePool CRD that is currently
	// deployed as a StatefulSet
	DeployedGeneration int64 `json:"deployedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EmbeddedBrokerPoolStatus defines the observed state of any node pools tied to this cluster
type EmbeddedBrokerPoolStatus struct {
	// Name is the name of the pool
	Name string `json:"name,omitempty"`
	// Replicas is the number of actual replicas currently across
	// the node pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32 `json:"replicas,omitempty"`
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all node pools.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their node pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32 `json:"outOfDateReplicas,omitempty"`
	// UpToDateReplicas is the number of replicas that currently match
	// their node pool definitions.
	UpToDateReplicas int32 `json:"upToDateReplicas,omitempty"`
	// CondemnedReplicas is the number of replicas that will be decommissioned
	// as part of a scaling down operation.
	CondemnedReplicas int32 `json:"condemnedReplicas,omitempty"`
	// ReadyReplicas is the number of replicas whose readiness probes are
	// currently passing.
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// RunningReplicas is the number of replicas that are actively in a running
	// state.
	RunningReplicas int32 `json:"runningReplicas,omitempty"`
}
