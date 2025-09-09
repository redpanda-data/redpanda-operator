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
	"crypto/tls"
	"fmt"
	"time"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow/adminv2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
)

// redpandaAdminForCluster returns a simple rpadmin.AdminAPI able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminForCluster(cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, error) {
	dot, err := cluster.GetDot(c.config)
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

// redpandaAdminV2ForCluster returns a simple rpadmin.AdminAPI able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) redpandaAdminV2ForCluster(cluster *redpandav1alpha2.Redpanda) (*adminv2.Client, error) {
	dot, err := cluster.GetDot(c.config)
	if err != nil {
		return nil, err
	}

	state, err := redpandachart.RenderStateFromDot(dot)
	if err != nil {
		return nil, err
	}
	info, err := redpanda.AdminClientConnectionInfo(state, c.dialer)
	if err != nil {
		return nil, err
	}

	if c.userAuth != nil {
		info.AdminAuthParams.Username = c.userAuth.Username
		info.AdminAuthParams.Password = c.userAuth.Password
	}

	builder := adminv2.NewClientBuilder(info.Hosts...).
		WithDialer(c.dialer).
		WithTLS(info.TLSConfig).
		WithClientTimeout(c.adminClientTimeout)

	if info.AdminAuthParams.Username != "" {
		builder = builder.WithBasicAuth(info.AdminAuthParams.Username, info.AdminAuthParams.Password)
	}

	return builder.Build()
}

func (c *Factory) redpandaAdminForV1Cluster(cluster *vectorizedv1alpha1.Cluster) (*rpadmin.AdminAPI, error) {
	ctx := context.Background()

	if cluster.AdminAPITLS() != nil {
		return nil, fmt.Errorf("non-TLS admin API is not supported on V1 CRD")
	}
	// Assume no TLS. Practically, we don't need to support it in Operator V1.
	t := &certmanager.ClusterCertificates{}

	a, err := admin.NewNodePoolInternalAdminAPI(ctx, c.Client, cluster, fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace), t, c.dialer, c.adminClientTimeout)
	if err != nil {
		return nil, err
	}
	// It's weird that the V1 admin factory returns an interface instead of the
	// rpadmin struct. We'll cast it back; it's ugly but since this is a legacy
	// code path we don't care too much.
	return a.(*rpadmin.AdminAPI), nil
}

func (c *Factory) redpandaAdminV2ForV1Cluster(cluster *vectorizedv1alpha1.Cluster) (*adminv2.Client, error) {
	ctx := context.Background()

	if cluster.AdminAPITLS() != nil {
		return nil, fmt.Errorf("non-TLS admin API is not supported on V1 CRD")
	}
	// Assume no TLS. Practically, we don't need to support it in Operator V1.
	t := &certmanager.ClusterCertificates{}
	timeout := c.adminClientTimeout
	fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace)

	adminInternal := cluster.AdminAPIInternal()
	if adminInternal == nil {
		return nil, &admin.NoInternalAdminAPI{}
	}

	var tlsConfig *tls.Config
	if adminInternal.TLS.Enabled {
		var err error
		tlsConfig, err = t.GetTLSConfig(ctx, c.Client)
		if err != nil {
			return nil, fmt.Errorf("could not create tls configuration for internal admin API: %w", err)
		}
	}

	urls := make([]string, 0)

	pods := []string{}
	var listedPods corev1.PodList
	err := c.Client.List(ctx, &listedPods, &client.ListOptions{
		LabelSelector: labels.ForCluster(cluster).AsClientSelector(),
		Namespace:     cluster.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("unable list pods to infer admin API URLs: %w", err)
	}

	for i := range listedPods.Items {
		pod := listedPods.Items[i]
		pods = append(pods, pod.Name)
	}

	for _, pod := range pods {
		urls = append(urls, fmt.Sprintf("%s.%s:%d", pod, fqdn, adminInternal.Port))
	}

	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return adminv2.NewClientBuilder(urls...).
		WithDialer(c.dialer).
		WithTLS(tlsConfig).
		WithClientTimeout(timeout).
		Build()
}

// schemaRegistryForCluster returns a simple sr.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) schemaRegistryForCluster(cluster *redpandav1alpha2.Redpanda) (*sr.Client, error) {
	dot, err := cluster.GetDot(c.config)
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

// kafkaForCluster returns a simple kgo.Client able to communicate with the given cluster specified via a Redpanda cluster.
func (c *Factory) kafkaForCluster(cluster *redpandav1alpha2.Redpanda, opts ...kgo.Opt) (*kgo.Client, error) {
	dot, err := cluster.GetDot(c.config)
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
