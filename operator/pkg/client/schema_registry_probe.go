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

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	if rp, ok := obj.(*redpandav1alpha2.Redpanda); ok && !redpandaSchemaRegistryEnabled(rp) {
		return nil, schemaregistry.ErrDisabled
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
