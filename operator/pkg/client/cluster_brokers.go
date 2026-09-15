// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"crypto/tls"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// ClusterBrokers describes a cluster's broker pods from the outside: how to
// tell them apart from everything else in the namespace, and the Schema
// Registry listener they serve. It is what a controller needs to decide, pod
// by pod, whether a pod is one of the cluster's brokers and whether that
// broker's Schema Registry is up.
type ClusterBrokers struct {
	// PodSelector is carried by every broker pod of the cluster, across node
	// pools. It is the cluster's own Service selector, so nothing the
	// operator renders matches it but brokers; anything else a user labels
	// this way is told apart by InternalService.
	PodSelector map[string]string
	// InternalService is the name of the cluster's internal headless
	// Service. Broker pods name it in spec.subdomain, which is what gives
	// them their <pod>.<service> DNS records -- so a pod under it is one the
	// cluster itself addresses as a broker, not merely one labelled like a
	// broker.
	InternalService string
	// ClusterDomain is the Kubernetes cluster domain the cluster was
	// configured with, completing <pod>.<InternalService>.<ns>.svc.<domain>.
	// Empty when the cluster kind doesn't record one (v1), in which case the
	// operator's own --cluster-domain applies.
	ClusterDomain string
	// SchemaRegistry is nil when the cluster's Schema Registry listener is
	// disabled. Its TLS configuration is resolved lazily, per probe.
	SchemaRegistry *schemaregistry.Listener
}

// ClusterBrokers returns the ClusterBrokers of a v1 Cluster, v2 Redpanda, or
// StretchCluster. For a StretchCluster the listener is read from a
// representative pool in clusterName -- the representative-pool model
// SchemaRegistryClientForCluster uses, with the same heterogeneous-pool
// caveat.
func (c *Factory) ClusterBrokers(ctx context.Context, obj any, clusterName string) (*ClusterBrokers, error) {
	switch cluster := obj.(type) {
	case *redpandav1alpha2.Redpanda:
		return c.redpandaClusterBrokers(ctx, cluster, clusterName)
	case *vectorizedv1alpha1.Cluster:
		return c.v1ClusterBrokers(ctx, cluster, clusterName)
	case *redpandav1alpha2.StretchCluster:
		return c.stretchClusterBrokers(ctx, cluster, clusterName)
	}
	return nil, errors.Newf("unsupported cluster object %T", obj)
}

func (c *Factory) redpandaClusterBrokers(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*ClusterBrokers, error) {
	state, err := c.redpandaRenderState(ctx, cluster, clusterName)
	if err != nil {
		return nil, err
	}

	brokers := &ClusterBrokers{
		PodSelector:     redpandachart.ClusterPodLabelsSelector(state),
		InternalService: redpandachart.ServiceName(state),
		ClusterDomain:   strings.TrimSuffix(state.Values.ClusterDomain, "."),
	}

	listener := state.Values.Listeners.SchemaRegistry
	if !listener.Enabled {
		return brokers, nil
	}
	brokers.SchemaRegistry = &schemaregistry.Listener{Port: listener.Port}
	if listener.TLS.IsEnabled(&state.Values.TLS) {
		brokers.SchemaRegistry.TLSConfig = memoizeTLS(func(context.Context) (*tls.Config, error) {
			return state.TLSConfig(listener.TLS)
		})
	}
	return brokers, nil
}

// memoizeTLS caches a successful TLS build, so a listener's certificates are
// read once however many brokers and ports are probed against them. A
// failure is not cached: a certificate that only shows up later is then
// picked up on the next probe rather than after the caller's cache expires.
func memoizeTLS(build func(ctx context.Context) (*tls.Config, error)) func(ctx context.Context) (*tls.Config, error) {
	var (
		mu     sync.Mutex
		config *tls.Config
	)
	return func(ctx context.Context) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		if config != nil {
			return config, nil
		}
		built, err := build(ctx)
		if err != nil {
			return nil, err
		}
		config = built
		return config, nil
	}
}

// redpandaRenderState renders the chart state of a v2 cluster, the view its
// internal clients are built from.
func (c *Factory) redpandaRenderState(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*redpandachart.RenderState, error) {
	config, err := c.GetConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}
	dot, err := cluster.GetDot(config)
	if err != nil {
		return nil, err
	}
	return redpandachart.RenderStateFromDot(dot)
}

// Everything a v1 cluster's shape depends on is in its spec, so no read --
// and no context -- is needed until a probe asks for the listener's TLS.
func (c *Factory) v1ClusterBrokers(_ context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string) (*ClusterBrokers, error) {
	brokers := &ClusterBrokers{
		PodSelector: labels.ForCluster(cluster).AsAPISelector().MatchLabels,
		// The v1 headless Service shares the Cluster's name; see
		// resources.HeadlessServiceResource.Key.
		InternalService: cluster.Name,
	}

	listener := cluster.SchemaRegistryInternalListener()
	if listener == nil {
		return brokers, nil
	}
	brokers.SchemaRegistry = &schemaregistry.Listener{Port: int32(listener.Port)}
	if listener.TLS == nil || !listener.TLS.Enabled {
		return brokers, nil
	}

	brokers.SchemaRegistry.TLSConfig = memoizeTLS(func(ctx context.Context) (*tls.Config, error) {
		k8sClient, err := c.GetClient(ctx, clusterName)
		if err != nil {
			return nil, err
		}
		_, certs, err := v1ClusterCerts(ctx, k8sClient, cluster)
		if err != nil {
			return nil, err
		}
		return certs.GetSchemaTLSConfig(ctx, k8sClient)
	})
	return brokers, nil
}

func (c *Factory) stretchClusterBrokers(ctx context.Context, sc *redpandav1alpha2.StretchCluster, clusterName string) (*ClusterBrokers, error) {
	k8sClient, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, errors.Wrap(err, "getting k8s client")
	}

	pool, err := c.representativeBrokerPool(ctx, sc, k8sClient)
	if err != nil {
		return nil, errors.Wrap(err, "finding representative broker pool")
	}
	if pool == nil {
		return nil, noRepresentativePoolError(sc)
	}
	poolSpec := defaultedPoolSpec(pool)

	brokers := &ClusterBrokers{
		PodSelector:     rendermulticluster.BrokerPodSelector(sc.Name),
		InternalService: rendermulticluster.ClusterServiceName(sc.Name),
		ClusterDomain:   strings.TrimSuffix(poolSpec.GetClusterDomain(), "."),
	}

	listener := poolSpec.Listeners.SchemaRegistry
	if !listener.IsEnabled() {
		return brokers, nil
	}
	brokers.SchemaRegistry = &schemaregistry.Listener{
		Port: poolSpec.SchemaRegistryPort(),
		TLSConfig: memoizeTLS(func(ctx context.Context) (*tls.Config, error) {
			return c.stretchClusterListenerTLSConfig(ctx, sc, poolFullnameFor(sc, pool), poolSpec, listener, k8sClient)
		}),
	}
	return brokers, nil
}
