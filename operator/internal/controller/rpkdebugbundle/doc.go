// Package rpkdebugbundle is an intentionally empty package that houses the
// kubebuilder annotations for generating the Roles required to run `rpk debug
// bundle`[1].
//
// [1]: https://github.com/redpanda-data/redpanda/blob/93edacb1d4c802c47d239cf7bbdc1660c869bd01/src/go/rpk/pkg/cli/debug/bundle/bundle_k8s_linux.go#L492-L501
package rpkdebugbundle

// +kubebuilder:rbac:groups="",namespace=default,resources=configmaps;endpoints;events;limitranges;persistentvolumeclaims;pods;pods/log;replicationcontrollers;resourcequotas;serviceaccounts;services,verbs=get;list
