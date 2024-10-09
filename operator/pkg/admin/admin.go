// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package admin contains tools for the operator to connect to the admin API
package admin

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/scalalang2/golang-fifo/sieve"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// NoInternalAdminAPI signal absence of the internal admin API endpoint
type NoInternalAdminAPI struct{}

func (n *NoInternalAdminAPI) Error() string {
	return "no internal admin API defined for cluster"
}

// CachedNodePoolAdminAPIClientFactory wraps an [AdminAPIClientFactory] with a caching
// layer to prevent leakage of memory.
//
// Memory can be easily leaked by [AdminAPIClient]s due to [http.Client]s
// connection pooling behavior and their lack of being exposed directly.
func CachedNodePoolAdminAPIClientFactory(factory NodePoolAdminAPIClientFactory) NodePoolAdminAPIClientFactory { //nolint:dupl // want to keep this for now
	// Mildly paranoid defaults, expire the client every 5 minutes so the
	// operator will continue to limp along in case something strange happens
	// (looking at you, coredns).
	cache := sieve.New[string, AdminAPIClient](75, 5*time.Minute)

	return func(
		ctx context.Context,
		k8sClient client.Reader,
		redpandaCluster *vectorizedv1alpha1.Cluster,
		fqdn string,
		adminTLSProvider types.AdminTLSConfigProvider,
		pods ...string,
	) (AdminAPIClient, error) {
		// Most importantly, Generation is part of the cache key. Anytime .Spec
		// changes, Generation will increment meaning we may need to
		// reconfigure the client due to changes in TLS/Auth/Etc.
		key := fmt.Sprintf("%s-%s-%d-%s-%v", redpandaCluster.Namespace, redpandaCluster.Name, redpandaCluster.Generation, fqdn, pods)

		if client, ok := cache.Get(key); ok { //nolint:gocritic // Shadowing client isn't a big deal in a wrapper.
			return client, nil
		}

		client, err := factory(ctx, k8sClient, redpandaCluster, fqdn, adminTLSProvider, pods...) //nolint:gocritic // Same as above.
		if err != nil {
			return nil, err
		}

		cache.Set(key, client)
		return client, nil
	}
}

// NewNodePoolInternalAdminAPI is used to construct a nodepool specific admin API client that talks to the cluster via
// the internal interface.
func NewNodePoolInternalAdminAPI(
	ctx context.Context,
	k8sClient client.Reader,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	adminTLSProvider types.AdminTLSConfigProvider,
	pods ...string,
) (AdminAPIClient, error) {
	adminInternal := redpandaCluster.AdminAPIInternal()
	if adminInternal == nil {
		return nil, &NoInternalAdminAPI{}
	}

	var tlsConfig *tls.Config
	if adminInternal.TLS.Enabled {
		var err error
		tlsConfig, err = adminTLSProvider.GetTLSConfig(ctx, k8sClient)
		if err != nil {
			return nil, fmt.Errorf("could not create tls configuration for internal admin API: %w", err)
		}
	}

	adminInternalPort := adminInternal.Port

	urls := make([]string, 0)

	if len(pods) == 0 {
		// If none provided, just add a URL for every Pod.
		var listedPods corev1.PodList
		err := k8sClient.List(ctx, &listedPods, &client.ListOptions{
			LabelSelector: labels.ForCluster(redpandaCluster).AsClientSelector(),
			Namespace:     redpandaCluster.Namespace,
		})
		if err != nil {
			return nil, fmt.Errorf("unable list pods to infer admin API URLs: %w", err)
		}

		for i := range listedPods.Items {
			pod := listedPods.Items[i]
			pods = append(pods, pod.Name)
		}
	}

	for _, pod := range pods {
		urls = append(urls, fmt.Sprintf("%s.%s:%d", pod, fqdn, adminInternalPort))
	}

	adminAPI, err := rpadmin.NewAdminAPI(urls, &rpadmin.NopAuth{}, tlsConfig) // If RPAdmin receives support for ServiceDiscovery, we can leverage it here.
	if err != nil {
		return nil, fmt.Errorf("error creating admin api for cluster %s/%s using urls %v (tls=%v): %w", redpandaCluster.Namespace, redpandaCluster.Name, urls, tlsConfig != nil, err)
	}

	return adminAPI, nil
}

// AdminAPIClient is a sub interface of the admin API containing what we need in the operator
//

type AdminAPIClient interface {
	Config(ctx context.Context, includeDefaults bool) (rpadmin.Config, error)
	ClusterConfigStatus(ctx context.Context, sendToLeader bool) (rpadmin.ConfigStatusResponse, error)
	ClusterConfigSchema(ctx context.Context) (rpadmin.ConfigSchema, error)
	PatchClusterConfig(ctx context.Context, upsert map[string]interface{}, remove []string) (rpadmin.ClusterConfigWriteResult, error)
	GetNodeConfig(ctx context.Context) (rpadmin.NodeConfig, error)

	CreateUser(ctx context.Context, username, password, mechanism string) error
	ListUsers(ctx context.Context) ([]string, error)
	DeleteUser(ctx context.Context, username string) error
	UpdateUser(ctx context.Context, username, password, mechanism string) error

	GetFeatures(ctx context.Context) (rpadmin.FeaturesResponse, error)
	SetLicense(ctx context.Context, license io.Reader) error
	GetLicenseInfo(ctx context.Context) (rpadmin.License, error)

	Brokers(ctx context.Context) ([]rpadmin.Broker, error)
	Broker(ctx context.Context, nodeID int) (rpadmin.Broker, error)
	DecommissionBroker(ctx context.Context, node int) error
	RecommissionBroker(ctx context.Context, node int) error

	EnableMaintenanceMode(ctx context.Context, node int) error
	DisableMaintenanceMode(ctx context.Context, node int, useLeaderNode bool) error

	GetHealthOverview(ctx context.Context) (rpadmin.ClusterHealthOverview, error)
}

// NodePoolAdminAPIClientFactory is a node aware abstract constructor of admin API clients
type NodePoolAdminAPIClientFactory func(
	ctx context.Context,
	k8sClient client.Reader,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	adminTLSProvider types.AdminTLSConfigProvider,
	pods ...string,
) (AdminAPIClient, error)

var _ NodePoolAdminAPIClientFactory = NewNodePoolInternalAdminAPI
