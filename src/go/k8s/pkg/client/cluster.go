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
	"github.com/redpanda-data/console/backend/pkg/config"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// RedpandaAdminForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
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

// KafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) kafkaForCluster(cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kgo.Client, error) {
	dot, err := cluster.GetDot(c.config)
	if err != nil {
		return nil, err
	}

	client, err := redpanda.KafkaClient(dot, c.dialer, opts...)
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		auth := scram.Auth{
			User: c.userAuth.Username,
			Pass: c.userAuth.Password,
		}

		var mechanism sasl.Mechanism
		switch c.userAuth.Mechanism {
		case config.SASLMechanismScramSHA256:
			mechanism = auth.AsSha256Mechanism()
		case config.SASLMechanismScramSHA512:
			mechanism = auth.AsSha512Mechanism()
		default:
			return nil, ErrUnsupportedSASLMechanism
		}

		return kgo.NewClient(append(client.Opts(), kgo.SASL(mechanism))...)
	}

	return client, nil
}
