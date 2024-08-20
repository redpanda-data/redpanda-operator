package v1alpha2

import "sigs.k8s.io/controller-runtime/pkg/client"

// The interfaces below are leveraged for consistently
// creating clients to the internal cluster APIs in
// our internal client factory code. Any CRD for which
// reconciliation requires configuring a cluster should
// implement some subset of these interfaces.

// KafkaConnectedObject is an interface for an object
// that specifies connection parameters to a Kafka API
// in the form of a KafkaAPISpec somewhere in its CRD definition.
// +kubebuilder:object:generate=false
type KafkaConnectedObject interface {
	client.Object
	GetKafkaAPISpec() *KafkaAPISpec
}

// KafkaConnectedObjectWithMetrics is an interface for an object
// that specifies connection parameters to a Kafka API in the form
// of a KafkaAPISpec somewhere in its CRD definition as well as
// an overriding namespace for Kafka client metrics.
// +kubebuilder:object:generate=false
type KafkaConnectedObjectWithMetrics interface {
	KafkaConnectedObject
	GetMetricsNamespace() *string
}

// AdminConnectedObject is an interface for an object
// that specifies connection parameters to a Redpanda Admin API
// in the form of an AdminAPISpec somewhere in its CRD definition.
// +kubebuilder:object:generate=false
type AdminConnectedObject interface {
	client.Object
	GetAdminAPISpec() *AdminAPISpec
}

// ClusterReferencingObject is an interface for an object
// that specifies connection parameters to a Redpanda cluster,
// (both the Kafka and Admin APIs) in the form of an ClusterRef
// somewhere in its CRD definition.
// +kubebuilder:object:generate=false
type ClusterReferencingObject interface {
	client.Object
	GetClusterRef() *ClusterRef
}
