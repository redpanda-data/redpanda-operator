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
	"slices"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

// ClusterBrokers describes a cluster's broker pods from the outside: how to
// tell them from everything else in the namespace, and the Schema Registry
// listener they serve.
//
// NB: accessors rather than fields, so the endpoint steering controller can
// name the shape it needs as an interface instead of importing this package.
// One value is shared across concurrent checks, so it is read-only once
// built.
type ClusterBrokers struct {
	podSelector         map[string]string
	clusterDomain       string
	schemaRegistry      *schemaregistry.Listener
	schemaRegistryPorts []int32
}

// PodSelector is the cluster's own Service selector, carried by every one of
// its broker pods across node pools. Nothing else the operator renders
// matches it.
func (c *ClusterBrokers) PodSelector() map[string]string {
	return c.podSelector
}

// ClusterDomain completes a broker pod's <pod>.<subdomain>.<ns>.svc.<domain>
// record. Empty when the cluster records none (v1), in which case the
// operator's own --cluster-domain applies.
func (c *ClusterBrokers) ClusterDomain() string {
	return c.clusterDomain
}

// SchemaRegistry is the listener to probe, nil when the cluster's internal
// listener is disabled. Its TLS configuration is resolved lazily, per probe.
func (c *ClusterBrokers) SchemaRegistry() *schemaregistry.Listener {
	return c.schemaRegistry
}

// SchemaRegistryPorts is every port a broker serves the registry on,
// internal and external alike. One registry backs them all -- the listeners
// are a list over a single store -- so one probe decides the lot, and an
// external port left out would publish a replaying broker to exactly the
// clients the internal port protects.
func (c *ClusterBrokers) SchemaRegistryPorts() []int32 {
	return c.schemaRegistryPorts
}

// ClusterBrokers returns the ClusterBrokers of a v1 Cluster or v2 Redpanda.
// A StretchCluster has none: endpoint steering, its only caller, leaves
// those Services to Kubernetes.
func (c *Factory) ClusterBrokers(ctx context.Context, obj any, clusterName string) (*ClusterBrokers, error) {
	switch cluster := obj.(type) {
	case *redpandav1alpha2.Redpanda:
		return c.redpandaClusterBrokers(ctx, cluster, clusterName)
	case *vectorizedv1alpha1.Cluster:
		return c.v1ClusterBrokers(ctx, cluster, clusterName)
	}
	return nil, errors.Newf("unsupported cluster object %T", obj)
}

func (c *Factory) redpandaClusterBrokers(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*ClusterBrokers, error) {
	state, err := c.redpandaRenderState(ctx, cluster, clusterName)
	if err != nil {
		return nil, err
	}

	brokers := &ClusterBrokers{
		podSelector:   redpandachart.ClusterPodLabelsSelector(state),
		clusterDomain: strings.TrimSuffix(state.Values.ClusterDomain, "."),
	}

	listener := state.Values.Listeners.SchemaRegistry
	if !listener.Enabled {
		return brokers, nil
	}
	brokers.schemaRegistryPorts = []int32{listener.Port}
	for name := range listener.External {
		// IsEnabled is the predicate the broker's own schema_registry_api
		// list is built from, so this is exactly what a broker binds.
		if external := listener.External[name]; external.IsEnabled() {
			brokers.schemaRegistryPorts = append(brokers.schemaRegistryPorts, external.Port)
		}
	}
	slices.Sort(brokers.schemaRegistryPorts)
	brokers.schemaRegistry = &schemaregistry.Listener{Port: listener.Port}
	if listener.TLS.IsEnabled(&state.Values.TLS) {
		brokers.schemaRegistry.TLSConfig = memoizeTLS(func(context.Context) (*tls.Config, error) {
			return state.TLSConfig(listener.TLS)
		})
	}
	return brokers, nil
}

// memoizeTLS caches a successful TLS build, so a listener's certificates are
// read once however many brokers and ports are probed against them. A
// failure is not cached, so a certificate that shows up late is picked up on
// the next probe.
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

// redpandaRenderState renders a v2 cluster's chart state, the view its
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

// Everything a v1 cluster's shape depends on is in its spec, so nothing is
// read -- and no context needed -- until a probe asks for the listener's TLS.
func (c *Factory) v1ClusterBrokers(_ context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string) (*ClusterBrokers, error) {
	brokers := &ClusterBrokers{
		podSelector: labels.ForCluster(cluster).AsAPISelector().MatchLabels,
	}

	brokers.schemaRegistryPorts = v1SchemaRegistryPorts(cluster)

	listener := cluster.SchemaRegistryInternalListener()
	if listener == nil {
		return brokers, nil
	}
	brokers.schemaRegistry = &schemaregistry.Listener{Port: int32(listener.Port)}
	if listener.TLS == nil || !listener.TLS.Enabled {
		return brokers, nil
	}

	brokers.schemaRegistry.TLSConfig = memoizeTLS(func(ctx context.Context) (*tls.Config, error) {
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

// v1SchemaRegistryPorts is the container port of every Schema Registry
// listener a v1 cluster declares, over both the current field and the one it
// replaced.
func v1SchemaRegistryPorts(cluster *vectorizedv1alpha1.Cluster) []int32 {
	var ports []int32
	for i := range cluster.Spec.Configuration.SchemaRegistryAPI {
		ports = append(ports, int32(cluster.Spec.Configuration.SchemaRegistryAPI[i].Port))
	}
	if legacy := cluster.Spec.Configuration.SchemaRegistry; legacy != nil {
		ports = append(ports, int32(legacy.Port))
	}
	return ports
}
