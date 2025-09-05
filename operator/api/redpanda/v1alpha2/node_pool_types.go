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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nodepools
// +kubebuilder:resource:shortName=np
// +kubebuilder:printcolumn:name="Bound",type="string",JSONPath=".status.conditions[?(@.type==\"Bound\")].status",description=""
// +kubebuilder:printcolumn:name="Deployed",type="string",JSONPath=".status.conditions[?(@.type==\"Deployed\")].status",description=""
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NodePoolSpec `json:"spec,omitempty"`
	// +kubebuilder:default={conditions: {{type: "Bound", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Deployed", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Quiesced", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}, {type: "Stable", status: "Unknown", reason: "NotReconciled", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status NodePoolStatus `json:"status,omitempty"`
}

func (n *NodePool) GetClusterSource() *ClusterSource {
	return &ClusterSource{
		ClusterRef: &n.Spec.ClusterRef,
	}
}

// NodePoolStatus defines the observed state of any node pools tied to this cluster
type NodePoolStatus struct {
	EmbeddedNodePoolStatus `json:",inline"`
	// DeployedGeneration represents the generation of the NodePool CRD that is currently
	// deployed as a StatefulSet
	DeployedGeneration int64 `json:"deployedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EmbeddedNodePoolStatus defines the observed state of any node pools tied to this cluster
type EmbeddedNodePoolStatus struct {
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

// +kubebuilder:object:root=true
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

func (s *NodePoolList) GetItems() []*NodePool {
	return functional.MapFn(ptr.To, s.Items)
}

// NodePoolSpec contains the node pool spec for the given node pool.
// Note that the defaulting behavior comes from the underlying Redpanda
// chart renderer, the attributes specified here will get merged in and
// override the defaults.
type NodePoolSpec struct {
	EmbeddedNodePoolSpec `json:",inline"`
	ClusterRef           ClusterRef `json:"clusterRef"`
}

type PoolConfigurator struct {
	// Chart default: []
	AdditionalCLIArgs []string `json:"additionalCLIArgs,omitempty"`
}

type PoolSetDataDirOwnership struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
}

type PoolFSValidator struct {
	// Chart default: false
	Enabled *bool `json:"enabled,omitempty"`
	// Chart default: xfs
	ExpectedFS *string `json:"expectedFS,omitempty"`
}

type PoolInitContainers struct {
	FSValidator         *PoolFSValidator         `json:"fsValidator,omitempty"`
	SetDataDirOwnership *PoolSetDataDirOwnership `json:"setDataDirOwnership,omitempty"`
	Configurator        *PoolConfigurator        `json:"configurator,omitempty"`
}

type EmbeddedNodePoolSpec struct {
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
	PodTemplate    *PodTemplate        `json:"podTemplate,omitempty"`
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
}

func MinimalNodePoolSpec(cluster *Redpanda) NodePoolSpec {
	return NodePoolSpec{
		ClusterRef: ClusterRef{
			Name: cluster.Name,
		},
		EmbeddedNodePoolSpec: EmbeddedNodePoolSpec{
			Replicas: ptr.To(int32(1)),
			Image: &RedpandaImage{
				Repository: ptr.To("redpandadata/redpanda"), // Use docker.io to make caching easier and to not inflate our own metrics.
			},
		},
	}
}
