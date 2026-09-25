// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package endpointsteering publishes the EndpointSlices of Services that opt
// in via [ServiceAnnotation], so that each of a Service's ports can be backed
// by a different subset of a Redpanda cluster's broker pods.
//
// The native EndpointSlice controller decides membership per pod: a pod is
// either behind every port of a Service or behind none. Redpanda brokers
// don't fit that model. A restarted broker serves Kafka as soon as it is up,
// but its Schema Registry replays the _schemas topic first and answers with
// errors until it has caught up -- and because broker discovery Services
// must publish not-ready addresses (Raft needs them), pod readiness cannot
// pull a replaying Schema Registry out of rotation without pulling Kafka
// with it (INC-2903, K8S-932). This controller instead asks each broker's
// Schema Registry directly and publishes the Schema Registry port only for
// brokers that answer.
//
// It is built on the common-go portmapper. A Service opts in by carrying
// [ServiceAnnotation] naming the cluster whose brokers back it and by
// defining no selector; the cluster's broker pods are found through the
// app.kubernetes.io/instance label every one of them already carries, so no
// pod template changes and nothing restarts.
//
// A cluster opts its own internal Services in with the
// feature.EndpointSteering annotation, which is what the v1 and v2
// renderers read before handing a Service to [Steer]. A Service the operator
// does not render -- a load balancer Service managed alongside it, say --
// opts in by carrying [ServiceAnnotation] itself.
//
// The controller therefore always runs: a Service that loses the annotation
// (a cluster whose flag was turned back off) needs it running to delete the
// slices it published and let the native controller take the Service back.
//
// One caveat on taking a Service over: the renderers drop spec.selector by
// omitting it from their server-side apply, which removes it only where the
// operator is its sole field manager. A selector co-owned by another manager
// survives, both controllers then publish, and the port-mapper says so with
// a warning Event on the Service.
//
// And one behavioral difference from the native controller: a pod that fails
// a check is left out of the slice, where the native controller would
// publish it with "ready: false". Traffic goes to the same places either
// way, since kube-proxy and CoreDNS route on readiness, but a consumer
// reading the slices sees an absence rather than an unready endpoint.
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
	// ServiceAnnotation opts a Service in. Its value names the cluster whose
	// brokers back the Service -- the v1 Cluster, v2 Redpanda, or
	// StretchCluster name, which is also the value of the brokers'
	// app.kubernetes.io/instance label. The Service must define no selector,
	// or the native controller publishes alongside this one.
	ServiceAnnotation = "cluster.redpanda.com/endpoints-for"

	// PodGroupLabel aligns pods with opted-in Services: a pod is a candidate
	// for a Service when this label equals the Service's annotation value.
	// The checker then confirms the pod really is a broker of that cluster.
	PodGroupLabel = labels.InstanceKey

	// ManagedBy is written to the endpointslice.kubernetes.io/managed-by
	// label of every published slice.
	ManagedBy = "redpanda-operator"

	// DefaultResyncPeriod bounds how long a broker whose Schema Registry has
	// come up (or gone down) stays unpublished (or published), since neither
	// changes anything the API server would report.
	DefaultResyncPeriod = 10 * time.Second
)

// Steer hands svc's EndpointSlices to this controller, which publishes them
// per port for the brokers of the named cluster. The selector goes, or the
// native EndpointSlice controller publishes every broker on every port
// alongside; the annotation names the cluster, and overrides any value a
// user set for the same key, since the controller relies on it.
//
// The v1 and v2 renderers apply it to whichever Service carries the
// cluster's Schema Registry listener, for a cluster that asked for it. A
// Helm release has nothing to publish its endpoints, so this is deliberately
// not part of the chart.
func Steer(svc *corev1.Service, cluster string) {
	svc.Spec.Selector = nil
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[ServiceAnnotation] = cluster
}

// Options configures Setup.
type Options struct {
	// Resolver maps a Service's annotation value to the cluster it names.
	// Required.
	Resolver Resolver
	// ClusterDomain is the Kubernetes cluster domain (kubelet's
	// --cluster-domain), used to name pods when verifying a TLS listener.
	ClusterDomain string
	// ResyncPeriod overrides DefaultResyncPeriod.
	ResyncPeriod time.Duration
}

// The Node permission only feeds topology zones onto published endpoints;
// the rest is what publishing EndpointSlices for a selectorless Service
// takes, including deleting the legacy Endpoints object the native
// controller abandons when a Service's selector is removed.
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
	// Service to no endpoints, so an empty set of resolvers is a
	// misconfiguration rather than a no-op.
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
		Membership:   portmapper.DeciderFunc(newChecker(opts.Resolver, opts.ClusterDomain).Decide),
		ResyncPeriod: resync,
		// Membership checks are network probes; with a single worker one
		// cluster full of replaying brokers would hold up every other
		// Service's resync.
		MaxConcurrentReconciles: 4,
	}, nil
}
