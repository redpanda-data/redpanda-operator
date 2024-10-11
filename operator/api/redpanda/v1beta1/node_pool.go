package v1beta1

import (
	// appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// func init() {
// 	appsv1.StatefulSetSpec
// }

// Post Statefulset's NodePools will become their own CRDs
// For now, they'll just map to Statefulsets.
type NodePoolSpec struct {
	// appsv1.StatefulSetSpec `json:",inline"` // TODO Which .Template "wins"?

	Template BrokerSpec `json:"template"`
}

type BrokerSpec struct {
	// Options:
	// Structured, keys like:
	// {advertised_kafka_api[0].name, "local"}
	// {advertised_kafka_api[0].address, "0.0.0."}
	// OR
	// {advertised_kafka_api[0], "{name: local, address: \"0.0.0.0\"}"}
	Config   []corev1.EnvVar

	// Straight up YAML string
	// values like:
	// advertised_kafka_api:
	// - name: local
	//   address: ${POD_NAME}.foo.bar.
	//   port: ${port+ORDINAL}
	ConfigStr string

	// Custom EnvVar like type that includes:
	// PerBroker []string

	Template corev1.PodTemplateSpec `json:"template"`
}

type NodePoolTemplate struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NodePoolSpec `json:"spec"`
}

// type NodePoolTemplate struct {
// 	metav1.TypeMeta   `json:",inline"`
// 	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
// 	Template          NodePoolSpecTemplate
// }
