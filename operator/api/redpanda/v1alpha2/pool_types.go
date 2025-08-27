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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nodepools
// +kubebuilder:resource:shortName=np
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodePoolSpec   `json:"spec,omitempty"`
	Status NodePoolStatus `json:"status,omitempty"`
}

// NodePoolStatus defines the observed state of any node pools tied to this cluster
type NodePoolStatus struct {
	// Name is the name of the pool
	Name string `json:"name"`
	// Replicas is the number of actual replicas currently across
	// the node pool. This differs from DesiredReplicas during
	// a scaling operation, but should be the same once the cluster
	// has quiesced.
	Replicas int32 `json:"replicas"`
	// DesiredReplicas is the number of replicas that ought to be
	// run for the cluster. It combines the desired replicas across
	// all node pools.
	DesiredReplicas int32 `json:"desiredReplicas"`
	// OutOfDateReplicas is the number of replicas that don't currently
	// match their node pool definitions. If OutOfDateReplicas is not 0
	// it should mean that the operator will soon roll this many pods.
	OutOfDateReplicas int32 `json:"outOfDateReplicas"`
	// UpToDateReplicas is the number of replicas that currently match
	// their node pool definitions.
	UpToDateReplicas int32 `json:"upToDateReplicas"`
	// CondemnedReplicas is the number of replicas that will be decommissioned
	// as part of a scaling down operation.
	CondemnedReplicas int32 `json:"condemnedReplicas"`
	// ReadyReplicas is the number of replicas whose readiness probes are
	// currently passing.
	ReadyReplicas int32 `json:"readyReplicas"`
	// RunningReplicas is the number of replicas that are actively in a running
	// state.
	RunningReplicas int32 `json:"runningReplicas"`
}

// +kubebuilder:object:root=true
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

type NodePoolSpec struct {
	EmbeddedNodePoolSpec `json:",inline"`
	ClusterRef           ClusterRef `json:"clusterRef"`
}

type PoolConfigurator struct {
	AdditionalCLIArgs []string `json:"additionalCLIArgs,omitempty"`
}

type PoolSetDataDirOwnership struct {
	Enabled *bool `json:"enabled,omitempty"`
}

type PoolFSValidator struct {
	Enabled    *bool   `json:"enabled,omitempty"`
	ExpectedFS *string `json:"expectedFS,omitempty"`
}

type PoolInitContainers struct {
	FSValidator         *PoolFSValidator         `json:"fsValidator,omitempty"`
	SetDataDirOwnership *PoolSetDataDirOwnership `json:"setDataDirOwnership,omitempty"`
	Configurator        *PoolConfigurator        `json:"configurator,omitempty"`
}

type EmbeddedNodePoolSpec struct {
	AdditionalSelectorLabels   map[string]string   `json:"additionalSelectorLabels,omitempty"`
	Replicas                   *int32              `json:"replicas,omitempty"`
	AdditionalRedpandaCmdFlags []string            `json:"additionalRedpandaCmdFlags,omitempty"`
	PodTemplate                *PodTemplate        `json:"podTemplate,omitempty"`
	Budget                     *Budget             `json:"budget,omitempty"`
	PodAntiAffinity            *PodAntiAffinity    `json:"podAntiAffinity,omitempty"`
	Sidecars                   *Sidecars           `json:"sideCars,omitempty"`
	InitContainers             *PoolInitContainers `json:"initContainers,omitempty"`
}

// Sidecars configures the additional sidecar containers that run alongside the main Redpanda container in the Pod.
type Sidecars struct {
	Image *RedpandaImage `json:"image,omitempty"`
	// Specifies additional volumes to mount to the sidecar.
	ExtraVolumeMounts *string `json:"extraVolumeMounts,omitempty"`
	// Specifies resource requests for the sidecar container.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Specifies the container's security context, including privileges and access levels of the container and its processes.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// +hidefromdoc
	Args []string `json:"args,omitempty"`
}
