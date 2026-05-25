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
	// Chart default: {}
	AdditionalSelectorLabels map[string]string `json:"additionalSelectorLabels,omitempty"`
	// Chart default: 3
	Replicas *int32 `json:"replicas,omitempty"`
	// Chart default: []
	AdditionalRedpandaCmdFlags []string `json:"additionalRedpandaCmdFlags,omitempty"`
	// Chart default:
	//     labels: {}
	//     annotations: {}
	//     spec:
	//       securityContext: {}
	//       affinity:
	//         podAntiAffinity:
	//           requiredDuringSchedulingIgnoredDuringExecution:
	//           - topologyKey: kubernetes.io/hostname
	//             labelSelector:
	//               matchLabels:
	//                 "app.kubernetes.io/component": '{{ include "redpanda.name" . }}-{{pool.name}}-statefulset'
	//                 "app.kubernetes.io/instance":  '{{ .Release.Name }}'
	//                 "app.kubernetes.io/name":      '{{ include "redpanda.name" . }}'
	//       terminationGracePeriodSeconds: 90
	//       nodeSelector: {}
	//       priorityClassName: ""
	//       tolerations: []
	//       topologySpreadConstraints:
	//       - maxSkew: 1
	//         topologyKey: topology.kubernetes.io/zone
	//         whenUnsatisfiable: ScheduleAnyway
	//         labelSelector:
	//           matchLabels:
	//             "app.kubernetes.io/component": '{{ include "redpanda.name" . }}-{{pool.name}}-statefulset'
	//             "app.kubernetes.io/instance":  '{{ .Release.Name }}'
	//             "app.kubernetes.io/name":      '{{ include "redpanda.name" . }}'
	PodTemplate *PodTemplate `json:"podTemplate,omitempty"`
	// Services configures overrides for Services created by the operator.
	Services       *NodePoolServices   `json:"services,omitempty"`
	InitContainers *PoolInitContainers `json:"initContainers,omitempty"`
	// Default:
	//     repository: docker.redpanda.com/redpandadata/redpanda
	//     tag: {{.redpandaVersion}}
	Image *RedpandaImage `json:"image,omitempty"`
	// Default:
	//     repository: docker.redpanda.com/redpandadata/redpanda-operator
	//     tag: {{.operatorVersion}}
	SidecarImage *RedpandaImage `json:"sidecarImage,omitempty"`
	// Chart default:
	//     repository: busybox
	//     tag: latest
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
