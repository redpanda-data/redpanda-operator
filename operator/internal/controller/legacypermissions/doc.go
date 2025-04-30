// Package legacypermissions, much like rpkdebugbundle and rackawareness is a
// faux package that only holds kubebuilder annotations. In the case of
// legacypermissions, it holds a set of permissions that are over scoped for
// the purpose of letting the operator deploy redpanda charts that contained
// over scoped permissions.
package legacypermissions

// +kubebuilder:rbac:groups="",resources=configmaps;endpoints;events;limitranges;persistentvolumeclaims;pods;pods/log;replicationcontrollers;resourcequotas;serviceaccounts;services,verbs=get;list
// +kubebuilder:rbac:groups="apps",namespace=default,resources=statefulsets/status,verbs=patch;update
// +kubebuilder:rbac:groups="",namespace=default,resources=persistentvolumeclaims,verbs=patch;update;watch;delete
