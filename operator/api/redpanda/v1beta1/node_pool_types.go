// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0
package v1beta1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
)

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
	Cluster              ClusterSource `json:"cluster"`
}

type EmbeddedNodePoolSpec struct {
	Replicas       *int32         `json:"replicas,omitempty"` // Default value? Required?
	BrokerTemplate BrokerTemplate `json:"brokerTemplate"`
}

type BrokerTemplate struct {
	Image     string                      `json:"image"`
	Resources corev1.ResourceRequirements `json:"resources"`

	// Arguments to be passed to rpk tune
	// https://docs.redpanda.com/current/reference/rpk/rpk-redpanda/rpk-redpanda-tune/
	Tuning []string `json:"tuning"`

	// This should have values similar to ClusterConfig so we can refer to external resources and use CEL functions.
	// TODO would Config be more or less clear here?
	NodeConfig map[string]ValueSource `json:"nodeConfig"`

	// Likely to be merged into NodeConfig w/ CEL functions.
	// rack: Expr(node_annotation('k8s.io/failure-domain')),
	// RackAwareness RackAwareness

	// According to Core Perf, this is no longer recommended.
	// Missing from chart. Punted to v25.2.
	// IOConfig any

	// Is there any reason to expose this?
	// RPKConfig map[string]any

	// InitContainer options e.g. set datadir ownership. FS validator.
	// TODO: These will be merged into the configurator container (NO MORE BASH!)
	SetDataDirOwnership bool `json:"setDataDirOwnership"`

	ValidateFSXFS bool `json:"validateFSXFS"`

	// Require volumes with special names to be provided.
	// datadir = required
	// ts-cache = optional tiered storage cache
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates"`

	PodTemplate *PodTemplate `json:"podTemplate"`
}

type PodTemplate struct {
	*applycorev1.PodTemplateApplyConfiguration `json:",inline"`
}

func (t *PodTemplate) DeepCopy() *PodTemplate {
	// For some inexplicable reason, apply configs don't have deepcopy
	// generated for them.
	//
	// DeepCopyInto can be generated with just DeepCopy implemented. Sadly, the
	// easiest way to implement DeepCopy is to run this type through JSON. It's
	// highly unlikely that we'll hit a panic but it is possible to do so with
	// invalid values for resource.Quantity and the like.
	out := new(PodTemplate)
	data, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}
