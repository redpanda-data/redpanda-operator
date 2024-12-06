// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"

	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
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
