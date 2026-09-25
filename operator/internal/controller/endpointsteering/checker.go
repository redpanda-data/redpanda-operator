// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/portmapper"
	"github.com/twmb/franz-go/pkg/sr"
	"golang.org/x/sync/singleflight"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
)

const (
	// clusterCacheTTL bounds how long a cluster's resolved shape (its pod
	// identity and Schema Registry listener) is reused before being read
	// again. Checks run for every pod and port on every resync, so resolving
	// per check would re-render and re-read the cluster at that rate.
	clusterCacheTTL = 30 * time.Second

	// clusterFailureTTL is how long a failed resolution is remembered, to
	// keep an unreadable cluster from re-resolving on every check of every
	// pod. Short, because it also bounds how long a cluster that has just
	// become readable keeps its old shape.
	clusterFailureTTL = 5 * time.Second

	// probeFailureThreshold is how many consecutive failed probes it takes
	// to unpublish a broker's Schema Registry, mirroring kubelet's default
	// readiness failureThreshold: a single slow answer under load must not
	// flap a healthy registry out of rotation.
	probeFailureThreshold = 3

	// probeSuccessThreshold is how many consecutive successful probes it
	// takes to publish it again. Kubelet's readiness equivalent is 1, but
	// kubelet is deciding one pod's condition where this decides an
	// EndpointSlice: a listener that comes and goes -- a restarting broker,
	// a store that syncs and falls behind again -- would otherwise be
	// written back in on every other probe, and each flip rewrites the
	// slice for every consumer of the Service. Requiring two consecutive
	// successes costs a recovered broker one resync, against a replay
	// measured in minutes, and keeps a genuinely flapping listener out
	// rather than oscillating.
	probeSuccessThreshold = 2
)

// checker is the portmapper membership decision for Redpanda broker pods. It
// publishes a pod for a Service port when the pod is one of the named
// cluster's brokers, the pod would be published by the native controller
// (Ready, or the Service publishes not-ready addresses), and -- for the
// cluster's Schema Registry port only -- the broker's Schema Registry answers
// GET /status/ready.
type checker struct {
	resolver      Resolver
	clusterDomain string
	probeTimeout  time.Duration
	// podReady is the native controller's inclusion rule, which every port
	// is still held to. It reads only the pod's conditions, so the Service's
	// publishNotReadyAddresses is applied around it.
	podReady portmapper.Checker
	// schemaRegistry damps probeSchemaRegistry's answers so a single slow
	// or dropped probe doesn't flap a healthy registry.
	schemaRegistry portmapper.Decider

	mu       sync.Mutex
	clusters map[string]clusterEntry
	inflight singleflight.Group
	now      func() time.Time
}

// clusterEntry is one cached resolution, successful or not.
type clusterEntry struct {
	brokers Cluster
	err     error
	expires time.Time
}

// newChecker returns a checker resolving clusters through resolver.
func newChecker(resolver Resolver, clusterDomain string) *checker {
	c := &checker{
		resolver:      resolver,
		clusterDomain: clusterDomain,
		probeTimeout:  schemaregistry.ProbeTimeout,
		podReady:      portmapper.PodReady(),
		clusters:      map[string]clusterEntry{},
		now:           time.Now,
	}
	// Stable's checker is always a Decider (that is how it damps an
	// [portmapper.Abstain] without resetting a streak); asserting it here is
	// what lets Decide consult it directly.
	c.schemaRegistry = portmapper.Stable(portmapper.DeciderFunc(c.probeSchemaRegistry), probeSuccessThreshold, probeFailureThreshold).(portmapper.Decider)
	return c
}

// Decide implements [portmapper.Decider]. The mapper's membership is typed
// [portmapper.Checker], so it is handed a [portmapper.DeciderFunc] over this;
// see mapperConfig.
func (c *checker) Decide(ctx context.Context, svc *corev1.Service, pod *corev1.Pod, port portmapper.Port) portmapper.Decision {
	logger := log.FromContext(ctx).WithValues("pod", client.ObjectKeyFromObject(pod), "port", port.Name, "targetPort", port.Port)

	brokers, err := c.brokers(ctx, pod.Namespace, groupOf(svc, pod))
	switch {
	case errors.Is(err, ErrUnknownCluster):
		logger.V(1).Info("excluding pod: no cluster found for the service's pod group")
		return portmapper.Exclude
	case err != nil:
		// A failed lookup says nothing about the pod; keep what's published.
		logger.V(1).Info("abstaining: resolving the pod's cluster failed", "error", err.Error())
		return portmapper.Abstain
	}

	if !isBroker(pod, brokers) {
		logger.V(2).Info("excluding pod: not a broker of the cluster")
		return portmapper.Exclude
	}

	// Every port is still held to the native controller's inclusion rule: a
	// pod backs a Service once it is Ready, or as soon as it has an address
	// when the Service publishes not-ready addresses -- which a broker
	// discovery Service must, since Raft needs members to resolve each other
	// before they are ready.
	if !svc.Spec.PublishNotReadyAddresses && !c.podReady.Check(ctx, svc, pod, port) {
		return portmapper.Exclude
	}
	// Ports other than Schema Registry are then published exactly as the
	// native controller would, so enabling steering changes nothing about
	// them.
	if !isSchemaRegistryPort(port, brokers) {
		return portmapper.Include
	}
	return c.schemaRegistry.Decide(ctx, svc, pod, port)
}

// groupOf is the pod group a membership check is about: the Service's
// annotation, which alignment guarantees equals the pod's label.
func groupOf(svc *corev1.Service, pod *corev1.Pod) string {
	if group := svc.Annotations[ServiceAnnotation]; group != "" {
		return group
	}
	return pod.Labels[PodGroupLabel]
}

// isBroker reports whether pod is one of brokers' pods, by the selector the
// cluster's own Services use: nothing the operator renders matches it but
// brokers.
func isBroker(pod *corev1.Pod, brokers Cluster) bool {
	return labels.SelectorFromSet(brokers.PodSelector()).Matches(labels.Set(pod.Labels))
}

func isSchemaRegistryPort(port portmapper.Port, brokers Cluster) bool {
	listener := brokers.SchemaRegistry()
	return listener != nil &&
		port.Protocol == corev1.ProtocolTCP &&
		port.Port == listener.Port
}

// probeSchemaRegistry asks the broker's Schema Registry whether its store has
// caught up, through a client scoped to this pod's address.
// /status/ready is auth-exempt and blocks until _schemas is replayed, so a
// timeout is the "still replaying" answer rather than a transport failure.
func (c *checker) probeSchemaRegistry(ctx context.Context, svc *corev1.Service, pod *corev1.Pod, port portmapper.Port) portmapper.Decision {
	logger := log.FromContext(ctx).WithValues("checker", "SchemaRegistryReady", "pod", client.ObjectKeyFromObject(pod), "port", port.Name, "targetPort", port.Port)

	brokers, err := c.brokers(ctx, pod.Namespace, groupOf(svc, pod))
	if err != nil || brokers.SchemaRegistry() == nil {
		// Decide already ruled on both cases; being here means the cache
		// turned over between the two lookups.
		return portmapper.Abstain
	}

	address := port.Address
	if address == "" {
		address = pod.Status.PodIP
	}
	broker, err := brokers.SchemaRegistry().BrokerAt(ctx, address, c.podDNSName(pod, brokers))
	if err != nil {
		logger.V(1).Info("abstaining: building the schema registry probe failed", "error", err.Error())
		return portmapper.Abstain
	}

	ctx, cancel := context.WithTimeout(ctx, c.probeTimeout)
	defer cancel()
	if synced, err := schemaregistry.Synced(ctx, []schemaregistry.Broker{broker}, logger); !synced {
		decision := classifyProbeError(err)
		logger.V(1).Info("schema registry not confirmed caught up", "url", broker.URL, "decision", decision.String())
		return decision
	}
	return portmapper.Include
}

// classifyProbeError separates failures that describe the broker from
// failures that describe the prober. A timeout is Schema Registry still
// replaying (or a broker that cannot be reached at all), a refused or
// dropped connection is its listener not being up, and a non-2xx is its own
// verdict: all three exclude, because an endpoint the operator cannot get a
// healthy answer out of is not one to send clients to, and [portmapper.Stable]
// damps the transient cases. Anything else -- a TLS verification failure, an
// unroutable pod network, a misconfigured transport -- says nothing about the
// broker and would read as "every broker is down" if it excluded, so it
// abstains and the previously published membership stands.
func classifyProbeError(err error) portmapper.Decision {
	var (
		netErr      net.Error
		responseErr *sr.ResponseError
	)
	switch {
	case errors.As(err, &responseErr):
		return portmapper.Exclude
	case errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return portmapper.Exclude
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF):
		return portmapper.Exclude
	}
	return portmapper.Abstain
}

// podDNSName is the pod's stable DNS name under its cluster's internal
// Service, which every implementation's broker certificates cover with a
// wildcard SAN. Empty for a pod that names no subdomain and so has no such
// record; the probe then verifies a TLS listener against the pod's address,
// which broker certificates never carry, and abstains.
func (c *checker) podDNSName(pod *corev1.Pod, brokers Cluster) string {
	if pod.Spec.Subdomain == "" {
		return ""
	}
	hostname := pod.Spec.Hostname
	if hostname == "" {
		hostname = pod.Name
	}
	domain := brokers.ClusterDomain()
	if domain == "" {
		domain = c.clusterDomain
	}
	return fmt.Sprintf("%s.%s.%s.svc.%s", hostname, pod.Spec.Subdomain, pod.Namespace, domain)
}

// brokers resolves group's cluster, caching the answer -- a failure included,
// briefly -- so that a cluster is read once per TTL however many pods and
// ports are checked against it. Concurrent misses for one cluster share a
// single resolution.
func (c *checker) brokers(ctx context.Context, namespace, group string) (Cluster, error) {
	key := namespace + "/" + group

	c.mu.Lock()
	entry, cached := c.clusters[key]
	fresh := cached && c.now().Before(entry.expires)
	c.mu.Unlock()
	if fresh {
		return entry.brokers, entry.err
	}

	result, err, _ := c.inflight.Do(key, func() (any, error) {
		brokers, err := c.resolver.Resolve(ctx, namespace, group)
		// A resolver answering with neither a cluster nor an error breaks
		// [Resolver]'s contract. Turn it into a failed lookup here, the one
		// place a Cluster enters the decision path: every consumer below is
		// then free to use it without a nil check, and the checker abstains
		// instead of panicking a port-mapper goroutine -- which would take
		// the manager down rather than fail one reconcile.
		if err == nil && brokers == nil {
			err = errors.Newf("resolver returned no cluster for %q and no error", key)
		}
		ttl := clusterCacheTTL
		if err != nil {
			ttl = clusterFailureTTL
			brokers = nil
		}
		c.mu.Lock()
		c.clusters[key] = clusterEntry{brokers: brokers, err: err, expires: c.now().Add(ttl)}
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return brokers, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(Cluster), nil
}
