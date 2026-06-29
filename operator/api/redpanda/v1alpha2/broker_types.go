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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// BrokerPhase describes the lifecycle phase of a Broker.
type BrokerPhase string

const (
	BrokerPhasePending         BrokerPhase = "Pending"
	BrokerPhaseProvisioning    BrokerPhase = "Provisioning"
	BrokerPhaseRunning         BrokerPhase = "Running"
	BrokerPhaseDecommissioning BrokerPhase = "Decommissioning"
	BrokerPhaseDecommissioned  BrokerPhase = "Decommissioned"
	BrokerPhaseStuck           BrokerPhase = "Stuck"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=brokers
// +kubebuilder:resource:shortName=brk
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="BrokerID",type="integer",JSONPath=".status.brokerID"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
type Broker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BrokerSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Ready", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "PodScheduled", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "StorageBound", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "BrokerRegistered", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status BrokerStatus `json:"status,omitempty"`
}

func (b *Broker) GetClusterSource() *ClusterSource {
	return &ClusterSource{
		ClusterRef: &b.Spec.ClusterRef,
	}
}

// +kubebuilder:object:root=true
type BrokerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Broker `json:"items"`
}

func (s *BrokerList) GetItems() []*Broker {
	return functional.MapFn(ptr.To, s.Items)
}

// BrokerSpec defines the desired state of a single Redpanda broker.
type BrokerSpec struct {
	// ClusterRef references the parent resource that owns this Broker.
	// Supported targets:
	//   - V1 Cluster: group=redpanda.vectorized.io, kind=Cluster
	//   - V2 Redpanda: group=cluster.redpanda.com (default), kind=Redpanda (default)
	//   - NodePool: group=cluster.redpanda.com (default), kind=NodePool
	// When pointing to a NodePool, the controller resolves the underlying
	// cluster through the NodePool's own ClusterRef.
	// +kubebuilder:validation:Required
	ClusterRef ClusterRef `json:"clusterRef"`

	// Decommission, when set to true, triggers the Broker controller to
	// decommission this broker via the Redpanda admin API. Decommission is
	// never triggered on raw CR deletion alone — only when this field is
	// explicitly set.
	// +optional
	Decommission bool `json:"decommission,omitempty"`

	// NetworkIndex is the stable external-addressing slot for this broker.
	// For ordinal-named pods this is the pod ordinal; the V2 chart uses it
	// to select external.addresses[networkIndex]. The value is captured from
	// the parsed ordinal at migration time and preserved across pod rotation.
	// +optional
	NetworkIndex *int32 `json:"networkIndex,omitempty"`

	// PodTemplate is the fully resolved pod spec for this broker.
	PodTemplate BrokerPodTemplate `json:"podTemplate"`

	// Storage defines PVC templates or references to existing claims.
	Storage BrokerStorage `json:"storage"`
}

// BrokerPodTemplate defines the pod metadata and spec for a broker.
// Uses flattened labels/annotations instead of nested ObjectMeta to avoid
// strict decoding issues with controller-gen's bare `metadata: type: object`.
type BrokerPodTemplate struct {
	// Labels to apply to the pod.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to apply to the pod.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Spec is the pod spec.
	Spec corev1.PodSpec `json:"spec"`
}

// BrokerStorage configures persistent storage for the broker.
type BrokerStorage struct {
	// VolumeClaimTemplates for new brokers (fresh PVC creation).
	// +optional
	VolumeClaimTemplates []BrokerVolumeClaim `json:"volumeClaimTemplates,omitempty"`
	// ExistingClaims for migrated brokers (references to StatefulSet-created PVCs).
	// +optional
	ExistingClaims []ExistingClaim `json:"existingClaims,omitempty"`
}

// BrokerVolumeClaim defines a PVC template with an explicit name field,
// avoiding the embedded ObjectMeta strict-decoding issue.
type BrokerVolumeClaim struct {
	// Name of the PVC to create.
	Name string `json:"name"`
	// Spec defines the desired characteristics of the volume.
	Spec corev1.PersistentVolumeClaimSpec `json:"spec"`
}

// ExistingClaim references a pre-existing PVC by name.
type ExistingClaim struct {
	// Name of the existing PVC.
	Name string `json:"name"`
	// MountPath inside the container.
	MountPath string `json:"mountPath"`
}

// BrokerStatus defines the observed state of a single Redpanda broker.
type BrokerStatus struct {
	// Phase is the high-level lifecycle state of the broker.
	// +optional
	Phase BrokerPhase `json:"phase,omitempty"`
	// BrokerID is the Redpanda node_id, discovered via the admin API after
	// the broker registers with the cluster. Nil until discovered.
	// +optional
	BrokerID *int32 `json:"brokerID,omitempty"`
	// PodName is the name of the pod managed by this Broker CR.
	// +optional
	PodName string `json:"podName,omitempty"`
	// PodIP is the IP address of the pod managed by this Broker CR.
	// +optional
	PodIP string `json:"podIP,omitempty"`
	// Conditions holds the conditions for the Broker.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
