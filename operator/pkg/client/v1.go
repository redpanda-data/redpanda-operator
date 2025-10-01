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
	"net"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"github.com/twmb/franz-go/pkg/sr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/certmanager"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

var (
	// NoKafkaAPI signal absence of the internal kafka API endpoint
	NoKafkaAPI = errors.New("no internal kafka API defined for cluster")
	// NoSchemaRegistryAPI signal absence of the internal kafka API endpoint
	NoSchemaRegistryAPI = errors.New("no internal schema registry API defined for cluster")
)

// newNodePoolInternalKafkaAPI is used to construct a nodepool specific admin API client that talks to the cluster via
// the internal interface.
func newNodePoolInternalKafkaAPI(
	ctx context.Context,
	k8sClient client.Client,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	tlsProvider resourcetypes.AdminTLSConfigProvider,
	dialer redpanda.DialContextFunc,
	opts []kgo.Opt,
	pods ...string,
) (*kgo.Client, error) {
	var err error

	kafkaAPI := redpandaCluster.InternalListener()
	if kafkaAPI == nil {
		return nil, NoKafkaAPI
	}

	if len(pods) == 0 {
		pods, err = v1PodNames(ctx, k8sClient, redpandaCluster)
		if err != nil {
			return nil, fmt.Errorf("unable list pods to infer kafka API URLs: %w", err)
		}
	}

	var tlsConfig *tls.Config
	if kafkaAPI.TLS.Enabled {
		tlsConfig, err = tlsProvider.GetKafkaTLSConfig(ctx, k8sClient)
		if err != nil {
			return nil, fmt.Errorf("could not create tls configuration for internal kafka API: %w", err)
		}
	}

	kafkaInternalPort := kafkaAPI.Port

	urls := make([]string, len(pods))
	for i, pod := range pods {
		urls[i] = fmt.Sprintf("%s.%s:%d", pod, fqdn, kafkaInternalPort)
	}

	opts = append(opts, kgo.SeedBrokers(urls...))

	if tlsConfig != nil {
		// we can only specify one of DialTLSConfig or Dialer
		if dialer == nil {
			opts = append(opts, kgo.DialTLSConfig(tlsConfig))
		} else {
			opts = append(opts, kgo.Dialer(wrapTLSDialer(dialer, tlsConfig)))
		}
	} else if dialer != nil {
		opts = append(opts, kgo.Dialer(dialer))
	}

	username, password, mechanism, err := v1ClusterAuth(ctx, k8sClient, redpandaCluster)
	if err != nil {
		return nil, err
	}

	if username != "" {
		opt, err := saslOpt(username, password, mechanism)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}

	return kgo.NewClient(opts...)
}

// newNodePoolInternalSchemaRegistryAPI is used to construct a nodepool specific admin API client that talks to the cluster via
// the internal interface.
func newNodePoolInternalSchemaRegistryAPI(
	ctx context.Context,
	k8sClient client.Client,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	tlsProvider resourcetypes.AdminTLSConfigProvider,
	dialer redpanda.DialContextFunc,
	opts []sr.ClientOpt,
	pods ...string,
) (*sr.Client, error) {
	var err error

	schemaRegistryAPI := redpandaCluster.SchemaRegistryInternalListener()
	if schemaRegistryAPI == nil {
		return nil, NoSchemaRegistryAPI
	}

	if len(pods) == 0 {
		pods, err = v1PodNames(ctx, k8sClient, redpandaCluster)
		if err != nil {
			return nil, fmt.Errorf("unable list pods to infer admin API URLs: %w", err)
		}
	}

	// These transport values come from the TLS client options found here:
	// https://github.com/twmb/franz-go/blob/cea7aa5d803781e5f0162187795482ba1990c729/pkg/sr/clientopt.go#L48-L68
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		DialContext:           dialer,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if dialer == nil {
		transport.DialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	prefix := "http://"

	var tlsConfig *tls.Config
	if schemaRegistryAPI.TLS != nil && schemaRegistryAPI.TLS.Enabled {
		tlsConfig, err = tlsProvider.GetSchemaTLSConfig(ctx, k8sClient)
		if err != nil {
			return nil, fmt.Errorf("could not create tls configuration for internal schema registry API: %w", err)
		}
		prefix = "https://"
		transport.TLSClientConfig = tlsConfig
	}

	copts := []sr.ClientOpt{sr.HTTPClient(&http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	})}

	schemaRegistryInternalPort := schemaRegistryAPI.Port

	urls := make([]string, len(pods))
	for i, pod := range pods {
		urls[i] = fmt.Sprintf("%s%s.%s:%d", prefix, pod, fqdn, schemaRegistryInternalPort)
	}

	copts = append(copts, sr.URLs(urls...))

	username, password, _, err := v1ClusterAuth(ctx, k8sClient, redpandaCluster)
	if err != nil {
		return nil, err
	}

	if username != "" {
		copts = append(copts, sr.BasicAuth(username, password))
	}

	// finally, override any calculated client opts with whatever was
	// passed in
	return sr.NewClient(append(copts, opts...)...)
}

func v1ClusterCerts(ctx context.Context, k8sClient client.Client, cluster *vectorizedv1alpha1.Cluster) (string, *certmanager.ClusterCertificates, error) {
	headlessSvc := resources.NewHeadlessService(k8sClient, cluster, controller.UnifiedScheme, nil, log.FromContext(ctx))
	clusterSvc := resources.NewClusterService(k8sClient, cluster, controller.UnifiedScheme, nil, log.FromContext(ctx))
	fqdn := headlessSvc.HeadlessServiceFQDN("cluster.local")
	clusterFQDN := clusterSvc.ServiceFQDN("cluster.local")
	certs, err := certmanager.NewClusterCertificates(ctx, cluster, certmanager.KeyStoreKey(cluster), k8sClient, fqdn, clusterFQDN, controller.UnifiedScheme, log.FromContext(ctx))
	if err != nil {
		return "", nil, err
	}
	return fqdn, certs, nil
}

func v1ClusterAuth(ctx context.Context, k8sClient client.Client, cluster *vectorizedv1alpha1.Cluster) (string, string, string, error) {
	if !cluster.IsSASLOnInternalEnabled() {
		return "", "", "", nil
	}

	superuser := resources.NewSuperUsers(k8sClient, cluster, controller.UnifiedScheme, resources.ScramRPKUsername, resources.RPKSuffix, log.FromContext(ctx))
	var superuserSecret corev1.Secret
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: superuser.Key().Namespace, Name: superuser.Key().Name}, &superuserSecret); err != nil {
		return "", "", "", err
	}
	username := string(superuserSecret.Data[corev1.BasicAuthUsernameKey])
	password := string(superuserSecret.Data[corev1.BasicAuthPasswordKey])
	// this looks hardcoded in the controller, see cluster_controller.go updateUserOnAdminAPI
	mechanism := rpadmin.ScramSha256

	return username, password, mechanism, nil
}

func v1PodNames(ctx context.Context, k8sClient client.Client, cluster *vectorizedv1alpha1.Cluster) ([]string, error) {
	names := []string{}

	var listedPods corev1.PodList
	err := k8sClient.List(ctx, &listedPods, &client.ListOptions{
		LabelSelector: labels.ForCluster(cluster).AsClientSelector(),
		Namespace:     cluster.Namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list pods for cluster: %w", err)
	}

	for i := range listedPods.Items {
		pod := listedPods.Items[i]
		names = append(names, pod.Name)
	}

	return names, nil
}

func saslOpt(user, password, mechanism string) (kgo.Opt, error) {
	var m sasl.Mechanism
	switch mechanism {
	case "SCRAM-SHA-256", "SCRAM-SHA-512":
		scram := scram.Auth{User: user, Pass: password}

		switch mechanism {
		case "SCRAM-SHA-256":
			m = scram.AsSha256Mechanism()
		case "SCRAM-SHA-512":
			m = scram.AsSha512Mechanism()
		}
	default:
		return nil, fmt.Errorf("unhandled SASL mechanism: %s", mechanism)
	}

	return kgo.SASL(m), nil
}
