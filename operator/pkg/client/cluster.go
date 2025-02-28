// Copyright 2025 Redpanda Data, Inc.
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

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
)

// redpandaAdminForCluster returns a simple rpadmin.AdminAPI able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminForCluster(cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, error) {
	dot, err := cluster.GetDot(c.config)
	if err != nil {
		return nil, err
	}

	client, err := redpanda.AdminClient(dot, c.dialer)
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

func (c *Factory) redpandaAdminForV1Cluster(cluster *vectorizedv1alpha1.Cluster) (*rpadmin.AdminAPI, error) {
	ctx := context.Background()

	if cluster.AdminAPITLS() != nil {
		return nil, fmt.Errorf("non-TLS admin API is not supported on V1 CRD")
	}
	// Assume no TLS. Practically, we don't need to support it in Operator V1.
	t := &certmanager.ClusterCertificates{}

	a, err := admin.NewNodePoolInternalAdminAPI(ctx, c.Client, cluster, fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace), t, c.dialer)
	if err != nil {
		return nil, err
	}
	// It's weird that the V1 admin factory returns an interface instead of the
	// rpadmin struct. We'll cast it back; it's ugly but since this is a legacy
	// code path we don't care too much.
	return a.(*rpadmin.AdminAPI), nil
}

// schemaRegistryForCluster returns a simple sr.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) schemaRegistryForCluster(cluster *redpandav1alpha2.Redpanda) (*sr.Client, error) {
	dot, err := cluster.GetDot(c.config)
	if err != nil {
		return nil, err
	}

	client, err := redpanda.SchemaRegistryClient(dot, c.dialer)
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

// kafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) kafkaForCluster(cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kgo.Client, error) {
	dot, err := cluster.GetDot(c.config)
	if err != nil {
		return nil, err
	}

	client, err := redpanda.KafkaClient(dot, c.dialer, opts...)
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
