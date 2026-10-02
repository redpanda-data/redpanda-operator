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
	"slices"
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
	// clusterCacheTTL is how long a cluster's resolved shape -- its pod
	// labels and Schema Registry listener -- is reused. Checks run per pod
	// per port per resync, so resolving per check would re-read the cluster
	// at that rate.
	clusterCacheTTL = 30 * time.Second

	// clusterFailureTTL is how long a failed resolution is remembered, so an
	// unreadable cluster isn't re-resolved on every check. Short, because it
	// also bounds how long a cluster that just became readable keeps its
	// stale shape.
	clusterFailureTTL = 5 * time.Second

	// probeFailureThreshold is how many failed probes in a row unpublish a
	// broker's Schema Registry, matching kubelet's readiness default so one
	// slow answer under load can't flap a healthy registry out.
	probeFailureThreshold = 3

	// probeSuccessThreshold is how many successes in a row publish it again.
	// Kubelet's equivalent is 1, but kubelet decides one pod's condition
	// where this rewrites endpoints every client of the Service reads, so a
	// listener that comes and goes would be written back in on every other
	// probe. Two costs a recovered broker one resync, against a replay
	// measured in minutes.
	probeSuccessThreshold = 2
)

// checker decides whether to publish one pod on one Service port. It says
// yes when the pod is one of the named cluster's brokers, Kubernetes would
// publish it too, and -- on the Schema Registry port only -- its Schema
// Registry answers GET /status/ready.
type checker struct {
	resolver     Resolver
	probeTimeout time.Duration
	// podReady is Kubernetes' own rule, which every port is still held to.
	// It reads the pod's conditions alone, so publishNotReadyAddresses is
	// applied around it.
	podReady portmapper.Checker
	// schemaRegistry damps probeSchemaRegistry so one slow or dropped probe
	// doesn't flap a healthy registry.
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
func newChecker(resolver Resolver) *checker {
	c := &checker{
		resolver:     resolver,
		probeTimeout: schemaregistry.ProbeTimeout,
		podReady:     portmapper.PodReady(),
		clusters:     map[string]clusterEntry{},
		now:          time.Now,
	}
	// NB: Stable always returns a Decider -- that is how it damps an Abstain
	// without breaking a streak -- and asserting it lets Decide call it
	// directly.
	c.schemaRegistry = portmapper.Stable(portmapper.DeciderFunc(c.probeSchemaRegistry), probeSuccessThreshold, probeFailureThreshold).(portmapper.Decider)
	return c
}

// Decide implements [portmapper.Decider].
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

	// Every port is still held to Kubernetes' own rule, so steering only
	// ever narrows what would be published. Broker discovery Services
	// publish not-ready addresses, since Raft members have to resolve each
	// other before they are ready.
	if !svc.Spec.PublishNotReadyAddresses && !c.podReady.Check(ctx, svc, pod, port) {
		return portmapper.Exclude
	}
	// Any other port is published just as Kubernetes would, so steering
	// changes nothing for it.
	if !isSchemaRegistryPort(port, brokers) {
		return portmapper.Include
	}
	return c.schemaRegistry.Decide(ctx, svc, pod, port)
}

// groupOf is the cluster a check is about: the Service's annotation, which
// pairing guarantees equals the pod's label.
func groupOf(svc *corev1.Service, pod *corev1.Pod) string {
	if group := svc.Annotations[ServiceAnnotation]; group != "" {
		return group
	}
	return pod.Labels[PodGroupLabel]
}

// isBroker reports whether pod is one of the cluster's brokers, by the
// selector the cluster's own Services use.
func isBroker(pod *corev1.Pod, brokers Cluster) bool {
	return labels.SelectorFromSet(brokers.PodSelector()).Matches(labels.Set(pod.Labels))
}

// isSchemaRegistryPort reports whether port is one a broker serves its
// registry on, internal or external.
//
// NB: a cluster with no internal listener is left ungated even if it has
// external ones. There is then nothing to probe, and excluding every broker
// would take the port down rather than steer it.
func isSchemaRegistryPort(port portmapper.Port, brokers Cluster) bool {
	return brokers.SchemaRegistry() != nil &&
		port.Protocol == corev1.ProtocolTCP &&
		slices.Contains(brokers.SchemaRegistryPorts(), port.Port)
}

// probeSchemaRegistry asks one broker's Schema Registry whether its store
// has caught up, over a client scoped to this pod's address.
//
// NB: /status/ready is auth-exempt and blocks until _schemas is replayed, so
// a timeout means "still replaying", not a broken connection.
func (c *checker) probeSchemaRegistry(ctx context.Context, svc *corev1.Service, pod *corev1.Pod, port portmapper.Port) portmapper.Decision {
	logger := log.FromContext(ctx).WithValues("checker", "SchemaRegistryReady", "pod", client.ObjectKeyFromObject(pod), "port", port.Name, "targetPort", port.Port)

	brokers, err := c.brokers(ctx, pod.Namespace, groupOf(svc, pod))
	if err != nil || brokers.SchemaRegistry() == nil {
		// Decide ruled on both already; being here means the cache turned
		// over in between.
		return portmapper.Abstain
	}

	address := port.Address
	if address == "" {
		address = pod.Status.PodIP
	}
	broker, err := brokers.SchemaRegistry().BrokerAt(ctx, address, podDNSName(pod, brokers))
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
// failures that describe the prober. A timeout (still replaying, or
// unreachable), a refused or dropped connection, and a non-2xx answer all
// exclude: an endpoint we can't get a healthy answer out of is not one to
// send clients to, and [portmapper.Stable] damps the transient cases.
// Anything else -- a TLS failure, an unroutable pod network, a broken
// transport -- says nothing about the broker and would read as "every broker
// is down", so it abstains and what is already published stands.
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
// Service, which broker certificates cover with a wildcard SAN. Empty for a
// pod naming no subdomain, which has no such record: a TLS listener is then
// verified against the pod's address, which no certificate carries, and the
// probe abstains.
func podDNSName(pod *corev1.Pod, brokers Cluster) string {
	if pod.Spec.Subdomain == "" {
		return ""
	}
	hostname := pod.Spec.Hostname
	if hostname == "" {
		hostname = pod.Name
	}
	return fmt.Sprintf("%s.%s.%s.svc.%s", hostname, pod.Spec.Subdomain, pod.Namespace, brokers.ClusterDomain())
}

// brokers resolves group's cluster, caching the answer -- failures included,
// briefly -- so a cluster is read once per TTL however many pods and ports
// are checked against it. Concurrent misses share one resolution.
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
		// NB: a resolver answering with neither a cluster nor an error
		// breaks [Resolver]'s contract. Turning it into a failure here, the
		// one place a Cluster enters, lets everything below skip the nil
		// check: a panic in a portmapper goroutine would take the manager
		// down, not just fail one reconcile.
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
