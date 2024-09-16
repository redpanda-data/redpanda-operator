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
	"encoding/json"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/console/backend/pkg/config"
	redpandachart "github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

func (c *Factory) dotFor(cluster *redpandav1alpha2.Redpanda) (*helmette.Dot, error) {
	var values []byte
	var partial redpandachart.PartialValues

	values, err := json.Marshal(cluster.Spec.ClusterSpec)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(values, &partial); err != nil {
		return nil, err
	}

	release := helmette.Release{
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
		Service:   "redpanda",
		IsInstall: true,
	}

	dot, err := redpandachart.Dot(release, partial)
	if err != nil {
		return nil, err
	}

	dot.KubeConfig = kube.RestToConfig(c.config)

	return dot, nil
}

// RedpandaAdminForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminForCluster(cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, error) {
	dot, err := c.dotFor(cluster)
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
	dot, err := c.dotFor(cluster)
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
