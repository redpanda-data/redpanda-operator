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

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/scalalang2/golang-fifo/sieve"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

// NoInternalAdminAPI signal absence of the internal admin API endpoint
type NoInternalAdminAPI struct{}

func (n *NoInternalAdminAPI) Error() string {
	return "no internal admin API defined for cluster"
}

// CachedAdminAPIClientFactory wraps an [AdminAPIClientFactory] with a caching
// layer to prevent leakage of memory.
//
// Memory can be easily leaked by [AdminAPIClient]s due to [http.Client]s
// connection pooling behavior and their lack of being exposed directly.
func CachedAdminAPIClientFactory(factory AdminAPIClientFactory) AdminAPIClientFactory {
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
		ordinals ...int32,
	) (AdminAPIClient, error) {
		// Most importantly, Generation is part of the cache key. Anytime .Spec
		// changes, Generation will increment meaning we may need to
		// reconfigure the client due to changes in TLS/Auth/Etc.
		key := fmt.Sprintf("%s-%s-%d-%s-%v", redpandaCluster.Namespace, redpandaCluster.Name, redpandaCluster.Generation, fqdn, ordinals)

		if client, ok := cache.Get(key); ok { //nolint:gocritic // Shadowing client isn't a big deal in a wrapper.
			return client, nil
		}

		client, err := factory(ctx, k8sClient, redpandaCluster, fqdn, adminTLSProvider, ordinals...) //nolint:gocritic // Same as above.
		if err != nil {
			return nil, err
		}

		cache.Set(key, client)
		return client, nil
	}
}

// NewInternalAdminAPI is used to construct an admin API client that talks to the cluster via
// the internal interface.
func NewInternalAdminAPI(
	ctx context.Context,
	k8sClient client.Reader,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	adminTLSProvider types.AdminTLSConfigProvider,
	ordinals ...int32,
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

	if len(ordinals) == 0 {
		// Not a specific node, just go through all them
		replicas := redpandaCluster.GetCurrentReplicas()

		for i := int32(0); i < replicas; i++ {
			ordinals = append(ordinals, i)
		}
	}
	urls := make([]string, 0, len(ordinals))
	for _, on := range ordinals {
		urls = append(urls, fmt.Sprintf("%s-%d.%s:%d", redpandaCluster.Name, on, fqdn, adminInternalPort))
	}

	adminAPI, err := rpadmin.NewAdminAPI(urls, &rpadmin.NopAuth{}, tlsConfig)
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

var _ AdminAPIClient = &rpadmin.AdminAPI{}

// AdminAPIClientFactory is an abstract constructor of admin API clients
//

type AdminAPIClientFactory func(
	ctx context.Context,
	k8sClient client.Reader,
	redpandaCluster *vectorizedv1alpha1.Cluster,
	fqdn string,
	adminTLSProvider types.AdminTLSConfigProvider,
	ordinals ...int32,
) (AdminAPIClient, error)

var _ AdminAPIClientFactory = NewInternalAdminAPI
