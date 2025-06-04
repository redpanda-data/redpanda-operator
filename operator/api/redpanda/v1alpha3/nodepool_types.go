// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha3

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

// +kubebuilder:object:root=true
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

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

type NodePoolSpec struct {
	EmbeddedNodePoolSpec `json:",inline"`
	ClusterRef           ClusterRef `json:"clusterRef"`
}

type EmbeddedNodePoolSpec struct {
	Replicas       *int32         `json:"replicas,omitempty"`
	BrokerTemplate BrokerTemplate `json:"brokerTemplate"`
}

type BrokerTemplate struct {
	Image     string                      `json:"image"`
	Resources corev1.ResourceRequirements `json:"resources"`
	// Arguments to be passed to rpk tune
	// https://docs.redpanda.com/current/reference/rpk/rpk-redpanda/rpk-redpanda-tune/
	Tuning                    []string               `json:"tuning"`
	NodeConfig                map[string]ValueSource `json:"nodeConfig"`
	RPKConfig                 map[string]ValueSource `json:"rpkConfig"`
	SetDataDirectoryOwnership bool                   `json:"setDataDirectoryOwnership"`
	ValidateFilesystem        bool                   `json:"validateFilesystem"`

	// Require volumes with special names to be provided.
	// datadir = required
	// ts-cache = optional tiered storage cache
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates"`
	PodTemplate          *PodTemplate                   `json:"podTemplate"`

	// TODO flags??
	// Likely to be merged into NodeConfig w/ CEL functions.
	// rack: Expr(node_annotation('k8s.io/failure-domain')),
	// TODO deprecate and move me into CEL functions.
}
