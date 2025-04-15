// Package rackawareness is an intentionally empty package that houses the
// kubebuilder annotations for generating the ClusterRole required for the
// redpanda helm chart's rack-awareness feature. As the operator will need the
// same ClusterRole in order to create the ClusterRole for its deployments, it
// is housed in the operator.
package rackawareness

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get
