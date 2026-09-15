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
	"fmt"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/schemaregistry"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// SchemaRegistryBrokerClients returns one Schema Registry client per broker
// endpoint of the given cluster object (same object forms as
// SchemaRegistryClient), each scoped to a single broker URL so callers can
// hold each broker to its own verdict — see schemaregistry.PerBrokerClients.
//
// Returns schemaregistry.ErrDisabled when the cluster explicitly disables
// its Schema Registry listener (v2 Redpanda spec, or a v1 Cluster without an
// internal SR listener).
func (c *Factory) SchemaRegistryBrokerClients(ctx context.Context, obj any) ([]schemaregistry.Broker, error) {
	if rp, ok := obj.(*redpandav1alpha2.Redpanda); ok {
		if !redpandaSchemaRegistryEnabled(rp) {
			return nil, schemaregistry.ErrDisabled
		}
		srClient, err := c.redpandaSchemaRegistryBrokerClient(ctx, rp, mcmanager.LocalCluster)
		if err != nil {
			return nil, err
		}
		return schemaregistry.PerBrokerClients(srClient)
	}
	if vc, ok := obj.(*vectorizedv1alpha1.Cluster); ok && vc.SchemaRegistryInternalListener() == nil {
		return nil, schemaregistry.ErrDisabled
	}

	client, err := c.SchemaRegistryClient(ctx, obj)
	if err != nil {
		return nil, err
	}
	return schemaregistry.PerBrokerClients(client)
}

// redpandaSchemaRegistryBrokerClient builds a v2 cluster's Schema Registry
// client with one URL per broker pod that exists, rather than per SRV record
// like SchemaRegistryClient. Under endpoint steering the internal Service's
// Schema Registry port is published only for brokers whose store has caught
// up, so its SRV record omits exactly the brokers the rolling-restart gate
// has to wait for.
func (c *Factory) redpandaSchemaRegistryBrokerClient(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*sr.Client, error) {
	state, err := c.redpandaRenderState(ctx, cluster, clusterName)
	if err != nil {
		return nil, err
	}
	k8sClient, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	var pods corev1.PodList
	if err := k8sClient.List(ctx, &pods, client.InNamespace(cluster.Namespace), client.MatchingLabels(redpandachart.ClusterPodLabelsSelector(state))); err != nil {
		return nil, errors.Wrap(err, "listing broker pods")
	}
	hosts := schemaRegistryBrokerHosts(state, pods.Items)
	if len(hosts) == 0 {
		return nil, errors.Newf("no broker pods found for cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	var opts []sr.ClientOpt
	if c.userAuth != nil {
		opts = append(opts, sr.BasicAuth(c.userAuth.Username, c.userAuth.Password))
	}
	return redpanda.SchemaRegistryClientForHosts(state, c.dialer, hosts, opts...)
}

// schemaRegistryBrokerHosts returns the sorted "host:port" Schema Registry
// addresses of the broker pods among pods. Pods are narrowed to those under
// the internal Service -- which is both what makes the returned names
// resolvable and what tells a broker from any pod labelled like one -- and to
// those a connection can be established to at all,
// since the gate fails closed and a pod with no address (Pending, or
// finished) would defer the roll forever rather than for as long as it takes
// its store to catch up.
func schemaRegistryBrokerHosts(state *redpandachart.RenderState, pods []corev1.Pod) []string {
	var hosts []string
	for i := range pods {
		pod := &pods[i]
		if pod.Spec.Subdomain != redpandachart.ServiceName(state) || !podDialable(pod) {
			continue
		}
		hosts = append(hosts, fmt.Sprintf("%s.%s:%d", pod.Name, redpandachart.InternalDomain(state), state.Values.Listeners.SchemaRegistry.Port))
	}
	slices.Sort(hosts)
	return hosts
}

// redpandaSchemaRegistryEnabled reports whether a v2 cluster's Schema
// Registry listener is enabled. The chart defaults
// listeners.schemaRegistry.enabled to true, so every unset level of the spec
// chain means enabled.
func redpandaSchemaRegistryEnabled(rp *redpandav1alpha2.Redpanda) bool {
	if rp == nil || rp.Spec.ClusterSpec == nil {
		return true
	}
	listeners := rp.Spec.ClusterSpec.Listeners
	if listeners == nil || listeners.SchemaRegistry == nil {
		return true
	}
	return ptr.Deref(listeners.SchemaRegistry.Enabled, true)
}

// srProbeConfig is the slice of a stretch broker pool's Schema Registry
// listener config that per-broker probing depends on.
// schemaRegistryForStretchCluster builds its client from a single
// representative pool — one port, one TLS config, stamped onto every
// broker's endpoint — so per-broker probing is only sound when every pool
// agrees on these fields.
type srProbeConfig struct {
	enabled bool
	port    int32
	tls     bool
	cert    string
}

func poolSRProbeConfig(pool *redpandav1alpha2.RedpandaBrokerPool) srProbeConfig {
	// Compute on the DEFAULTED spec, matching what the renderer and this
	// factory see: MergeDefaults fills in Listeners.SchemaRegistry, so a
	// pool with no listener config at all is an enabled listener on the
	// default port, not a disabled one.
	spec := defaultedPoolSpec(pool)
	listener := spec.Listeners.SchemaRegistry

	cfg := srProbeConfig{
		enabled: listener.IsEnabled(),
		port:    spec.SchemaRegistryPort(),
	}
	if listener.IsTLSEnabled(spec.TLS) {
		cfg.tls = true
		cfg.cert = listener.TLS.GetCert()
	}
	return cfg
}

// SchemaRegistryProbeUniform reports whether per-broker Schema Registry
// probing (SchemaRegistryBrokerClients) is sound for a stretch cluster with
// the given broker pools, and a human-readable reason when it isn't. The
// stretch client is built from ONE representative pool, so probing is only
// sound when every pool's SR listener is enabled and identical in the
// probe-relevant fields: a pool that doesn't serve SR (explicitly disabled),
// serves it on a different port, or terminates TLS differently would probe
// as permanently unreachable.
func SchemaRegistryProbeUniform(pools []*redpandav1alpha2.RedpandaBrokerPool) (bool, string) {
	var first *srProbeConfig
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		cfg := poolSRProbeConfig(pool)
		if !cfg.enabled {
			return false, fmt.Sprintf("schema registry listener explicitly disabled on broker pool %s/%s", pool.Namespace, pool.Name)
		}
		if first == nil {
			first = &cfg
			continue
		}
		if cfg != *first {
			return false, fmt.Sprintf("broker pool %s/%s's schema registry listener config (port/TLS) differs from its peers; the probe client assumes a homogeneous listener", pool.Namespace, pool.Name)
		}
	}
	return true, ""
}

// NodePoolSchemaRegistryBrokerClients builds one Schema Registry client per
// broker pod of a v1 (vectorized) Cluster, scoped per broker URL for the
// rolling-restart gate. It matches resources.SchemaRegistryClientsFactory
// and is injected into the v1 reconciliation path from the run command —
// pkg/resources cannot import this package directly (this package imports
// pkg/resources), mirroring how adminutils.NodePoolAdminAPIClientFactory is
// wired.
//
// Returns schemaregistry.ErrDisabled when the cluster has no internal Schema
// Registry listener.
func NodePoolSchemaRegistryBrokerClients(
	ctx context.Context,
	k8sClient client.Client,
	cluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	tlsProvider resourcetypes.AdminTLSConfigProvider,
	dialer redpanda.DialContextFunc,
	pods ...string,
) ([]schemaregistry.Broker, error) {
	if cluster.SchemaRegistryInternalListener() == nil {
		return nil, schemaregistry.ErrDisabled
	}
	aggregate, err := newNodePoolInternalSchemaRegistryAPI(ctx, k8sClient, cluster, fqdn, tlsProvider, dialer, nil, pods...)
	if err != nil {
		return nil, err
	}
	return schemaregistry.PerBrokerClients(aggregate)
}
