package v1beta1

import (
	// appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Typing Principals: Ensure the schema is structured yet flexible enough to handle most possible customizations.
// two other CRDs will be layed on top:
// 1. A major version specific generated one.
// 2. An unstructured CRD that get's massaged into the correct type.
// IDEA: Could have a single reconciler that just performs a type switch and manages mappings from A -> B.

// Naming reasoning: RedpandaCluster is less ambiguous than "Redpanda" or
// "Cluster". The casing and lack of a space should make it immediately
// apparent that a specific proper noun is being referred to. e.g. "We need the
// RedpandaCluster" vs "We need the Redpanda" vs "We need the Cluster".
type RedpandaCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda cluster.
	Spec RedpandaClusterSpec `json:"spec,omitempty"`
	// Represents the current status of the Redpanda cluster.
	Status RedpandaClusterStatus `json:"status,omitempty"`
}

type RedpandaClusterSpec struct {
	// Holds anything from https://docs.redpanda.com/current/reference/properties/cluster-properties/
	Config    []corev1.EnvVar    `json:"config"` // TODO Extract into own type and remove references to pod specifics.
	NodePools []NodePoolTemplate `json:"nodePools"`
}

type RedpandaClusterStatus struct {
	ObservedGeneration int64 `json:"observedGeneration"`
	// No strong feelings about what should exist here otherwise.
}
