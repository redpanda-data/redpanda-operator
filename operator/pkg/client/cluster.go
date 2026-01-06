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

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
)

// redpandaAdminForCluster returns a simple rpadmin.AdminAPI able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*rpadmin.AdminAPI, error) {
	config, err := c.GetConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	dot, err := cluster.GetDot(config)
	if err != nil {
		return nil, err
	}

	state, err := redpandachart.RenderStateFromDot(dot)
	if err != nil {
		return nil, err
	}
	client, err := redpanda.AdminClient(state, c.dialer, rpadmin.ClientTimeout(c.adminClientTimeout))
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		client.SetAuth(&rpadmin.BasicAuth{
			Username: c.userAuth.Username,
			Password: c.userAuth.Password,
		})
	}

	return client, nil
}

func (c *Factory) redpandaAdminForV1Cluster(ctx context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string) (*rpadmin.AdminAPI, error) {
	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	if cluster.AdminAPITLS() != nil {
		return nil, fmt.Errorf("non-TLS admin API is not supported on V1 CRD")
	}
	// Assume no TLS. Practically, we don't need to support it in Operator V1.
	t := &certmanager.ClusterCertificates{}

	a, err := admin.NewNodePoolInternalAdminAPI(ctx, client, cluster, fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace), t, c.dialer, c.adminClientTimeout)
	if err != nil {
		return nil, err
	}
	// It's weird that the V1 admin factory returns an interface instead of the
	// rpadmin struct. We'll cast it back; it's ugly but since this is a legacy
	// code path we don't care too much.
	return a.(*rpadmin.AdminAPI), nil
}

// schemaRegistryForCluster returns a simple sr.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) schemaRegistryForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (*sr.Client, error) {
	config, err := c.GetConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	dot, err := cluster.GetDot(config)
	if err != nil {
		return nil, err
	}

	state, err := redpandachart.RenderStateFromDot(dot)
	if err != nil {
		return nil, err
	}
	client, err := redpanda.SchemaRegistryClient(state, c.dialer)
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		client, err = sr.NewClient(append(client.Opts(), sr.BasicAuth(c.userAuth.Username, c.userAuth.Password))...)
		if err != nil {
			return nil, err
		}
	}

	return client, nil
}

func (c *Factory) schemaRegistryForV1Cluster(ctx context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string) (*sr.Client, error) {
	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	fqdn, certs, err := v1ClusterCerts(ctx, client, cluster)
	if err != nil {
		return nil, err
	}

	srClient, err := newNodePoolInternalSchemaRegistryAPI(ctx, client, cluster, fqdn, certs, c.dialer, nil)
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		srClient, err = sr.NewClient(append(srClient.Opts(), sr.BasicAuth(c.userAuth.Username, c.userAuth.Password))...)
		if err != nil {
			return nil, err
		}
	}

	return srClient, nil
}

// kafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) kafkaForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string, opts ...kgo.Opt) (*kgo.Client, error) {
	config, err := c.GetConfig(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	dot, err := cluster.GetDot(config)
	if err != nil {
		return nil, err
	}

	state, err := redpandachart.RenderStateFromDot(dot)
	if err != nil {
		return nil, err
	}
	client, err := redpanda.KafkaClient(state, c.dialer, opts...)
	if err != nil {
		return nil, err
	}

	authOpt, err := c.kafkaUserAuth()
	if err != nil {
		// close the client since it's no longer usable
		client.Close()

		return nil, err
	}

	if authOpt != nil {
		// close this client since we're not going to use it anymore
		client.Close()

		return kgo.NewClient(append(client.Opts(), authOpt)...)
	}

	return client, nil
}

func (c *Factory) remoteClusterSettingsForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, clusterName string) (shadow.RemoteClusterSettings, error) {
	var settings shadow.RemoteClusterSettings

	config, err := c.GetConfig(ctx, clusterName)
	if err != nil {
		return settings, err
	}

	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return settings, err
	}

	dot, err := cluster.GetDot(config)
	if err != nil {
		return settings, err
	}

	state, err := redpandachart.RenderStateFromDot(dot)
	if err != nil {
		return settings, err
	}

	clusterConfig, err := state.AsStaticConfigSource().Kafka.Load(ctx, client, c.secretExpander)
	if err != nil {
		return settings, err
	}
	settings.BootstrapServers = clusterConfig.Brokers
	if clusterConfig.TLS != nil {
		settings.TLSSettings = &shadow.TLSSettings{
			CA:   clusterConfig.TLS.CA,
			Cert: clusterConfig.TLS.Cert,
			Key:  clusterConfig.TLS.Key,
		}
	}

	if clusterConfig.SASL != nil {
		settings.Authentication = &shadow.AuthenticationSettings{
			Username:  clusterConfig.SASL.Username,
			Password:  clusterConfig.SASL.Password,
			Mechanism: redpandav1alpha2.SASLMechanism(clusterConfig.SASL.Mechanism),
		}
	}

	return settings, nil
}

func (c *Factory) kafkaForV1Cluster(ctx context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string, opts ...kgo.Opt) (*kgo.Client, error) {
	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	fqdn, certs, err := v1ClusterCerts(ctx, client, cluster)
	if err != nil {
		return nil, err
	}

	kClient, err := newNodePoolInternalKafkaAPI(ctx, client, cluster, fqdn, certs, c.dialer, opts)
	if err != nil {
		return nil, err
	}

	authOpt, err := c.kafkaUserAuth()
	if err != nil {
		// close the client since it's no longer usable
		kClient.Close()

		return nil, err
	}

	if authOpt != nil {
		// close this client since we're not going to use it anymore
		kClient.Close()

		return kgo.NewClient(append(kClient.Opts(), authOpt)...)
	}

	return kClient, nil
}

func (c *Factory) remoteClusterSettingsForV1Cluster(ctx context.Context, cluster *vectorizedv1alpha1.Cluster, clusterName string) (shadow.RemoteClusterSettings, error) {
	var settings shadow.RemoteClusterSettings

	client, err := c.GetClient(ctx, clusterName)
	if err != nil {
		return settings, err
	}

	fqdn, certs, err := v1ClusterCerts(ctx, client, cluster)
	if err != nil {
		return settings, err
	}

	return remoteClusterSettingsFromV1(ctx, client, cluster, fqdn, certs)
}
