package client

import (
	"context"
	"encoding/json"

	"github.com/redpanda-data/common-go/rpadmin"
	redpandachart "github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (c *clientFactory) dotFor(cluster *redpandav1alpha2.Redpanda) (*helmette.Dot, error) {
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
func (c *clientFactory) redpandaAdminForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, error) {
	dot, err := c.dotFor(cluster)
	if err != nil {
		return nil, err
	}

	return redpanda.AdminClient(dot, c.dialer)
}

// KafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *clientFactory) kafkaForCluster(ctx context.Context, cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kgo.Client, error) {
	dot, err := c.dotFor(cluster)
	if err != nil {
		return nil, err
	}

	return redpanda.KafkaClient(dot, c.dialer, opts...)
}
