// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package endpointsteering publishes the endpoints of Services that opt in
// via [ServiceAnnotation], so that each port of a Service can be backed by a
// different subset of a Redpanda cluster's brokers.
//
// Kubernetes decides endpoints per pod: a pod sits behind every port of a
// Service or none of them. Redpanda brokers don't fit that. A restarted
// broker serves Kafka immediately, but its Schema Registry replays the
// _schemas topic first and errors until it catches up. Readiness can't
// express the difference: broker discovery Services publish not-ready
// addresses because Raft needs them, and marking the pod unready would take
// Kafka down along with the registry (INC-2903, K8S-932). So this controller
// asks each broker's Schema Registry directly, and publishes the registry
// ports -- internal and external, one registry backs them all -- only for
// the brokers that answer.
//
// A Service opts in by naming its cluster in [ServiceAnnotation] and
// declaring no selector. Brokers are matched by the
// app.kubernetes.io/instance label they already carry, so turning steering
// on restarts nothing. The v1 and v2 renderers opt a cluster's own Service
// in through [Steer], for a cluster carrying the feature.EndpointSteering
// annotation; any other Service, a load balancer say, carries
// [ServiceAnnotation] itself. StretchCluster Services are left to
// Kubernetes: steering pods in several Kubernetes clusters from one operator
// needs the portmapper this is built on to reach them first.
//
// NB: the controller runs even with nothing opted in, because handing a
// Service back -- deleting what it published once the annotation goes away
// -- is its job too.
//
// NB: [Steer] drops spec.selector, which only sticks where the operator owns
// that field alone. A selector another manager co-owns survives, both
// publishers then write, and the portmapper raises a warning Event on the
// Service.
//
// NB: a pod that fails its check is left out of the endpoints, where
// Kubernetes would publish it as not ready. Traffic reaches the same places
// either way, but anything reading the endpoints sees an absence.
package endpointsteering

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/portmapper"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

const (
	// ServiceAnnotation opts a Service in. Its value is the name of the
	// cluster whose brokers back it, which is also those brokers'
	// app.kubernetes.io/instance label.
	ServiceAnnotation = "cluster.redpanda.com/endpoints-for"

	// PodGroupLabel pairs pods with opted-in Services: a pod is a candidate
	// when this label matches the Service's annotation. The checker then
	// confirms it really is a broker of that cluster.
	PodGroupLabel = labels.InstanceKey

	// ManagedBy goes in every published slice's
	// endpointslice.kubernetes.io/managed-by label.
	ManagedBy = "redpanda-operator"

	// DefaultResyncPeriod bounds how long a Schema Registry that has come up
	// or gone down waits to be noticed. Neither changes anything Kubernetes
	// would tell us about.
	DefaultResyncPeriod = 10 * time.Second
)

// Steer hands svc's endpoints to this controller, which publishes them per
// port for the named cluster's brokers. The v1 and v2 renderers call it on
// whichever Service carries the cluster's Schema Registry listener. The
// chart deliberately does not: a Helm release has no operator to publish
// its endpoints.
//
// NB: the selector has to go, or Kubernetes publishes every broker on every
// port alongside us, and the annotation overwrites any value a user set for
// the same key, since the controller depends on it.
func Steer(svc *corev1.Service, cluster string) {
	svc.Spec.Selector = nil
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[ServiceAnnotation] = cluster
}

// Options configures Setup.
type Options struct {
	// Resolver maps a Service's annotation to the cluster it names. Required.
	Resolver Resolver
	// ClusterDomain is kubelet's --cluster-domain, used to name pods when
	// verifying a TLS listener. It applies to clusters that record no domain
	// of their own; see withClusterDomain.
	ClusterDomain string
	// ResyncPeriod overrides DefaultResyncPeriod.
	ResyncPeriod time.Duration
}

// Nodes only feed topology zones onto published endpoints. The rest is what
// publishing endpoints for a selectorless Service takes, down to deleting
// the legacy Endpoints object Kubernetes abandons when a selector goes away.
//
// +kubebuilder:rbac:groups="",resources=services;pods;nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch;create;update;patch;delete

// Setup registers the endpoint steering controller with mgr.
func Setup(mgr ctrl.Manager, opts Options) error {
	cfg, err := mapperConfig(opts)
	if err != nil {
		return err
	}
	mapper, err := portmapper.New(cfg)
	if err != nil {
		return err
	}
	return mapper.SetupWithManager(mgr)
}

// mapperConfig translates opts into the portmapper's configuration.
func mapperConfig(opts Options) (portmapper.Config, error) {
	// A resolver that can never name a cluster would drain every opted-in
	// Service, so no resolvers at all is an error rather than a no-op.
	if resolvers, ok := opts.Resolver.(Resolvers); ok && len(resolvers) == 0 {
		opts.Resolver = nil
	}
	if opts.Resolver == nil {
		return portmapper.Config{}, errors.New("endpoint steering requires a cluster resolver")
	}
	resync := opts.ResyncPeriod
	if resync <= 0 {
		resync = DefaultResyncPeriod
	}

	return portmapper.Config{
		ManagedBy:    ManagedBy,
		ServiceKey:   portmapper.AnnotationKey(ServiceAnnotation),
		PodKey:       portmapper.LabelKey(PodGroupLabel),
		Membership:   portmapper.DeciderFunc(newChecker(withClusterDomain(opts.Resolver, opts.ClusterDomain)).Decide),
		ResyncPeriod: resync,
		// Checks are network probes; with one worker a single cluster full
		// of replaying brokers would hold up every other Service.
		MaxConcurrentReconciles: 4,
	}, nil
}
