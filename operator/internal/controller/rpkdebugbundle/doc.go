// Package rpkdebugbundle is an intentionally empty package that houses the
// kubebuilder annotations for generating the Roles required to run `rpk debug
// bundle`[1].
//
// [1]: https://github.com/redpanda-data/redpanda/blob/93edacb1d4c802c47d239cf7bbdc1660c869bd01/src/go/rpk/pkg/cli/debug/bundle/bundle_k8s_linux.go#L492-L501
package rpkdebugbundle

// +kubebuilder:rbac:groups="",namespace=default,resources=configmaps;endpoints;events;limitranges;persistentvolumeclaims;pods;pods/log;replicationcontrollers;resourcequotas;serviceaccounts;services,verbs=get;list

// HACK / REMOVE ME SOON: This false set of permissions is here to be in sync
// with the redpanda chart. They are all superfluous.
// +kubebuilder:rbac:groups="",resources=nodes;configmaps;endpoints;events;limitranges;persistentvolumeclaims;pods;pods/log;replicationcontrollers;resourcequotas;serviceaccounts;services,verbs=get;list
