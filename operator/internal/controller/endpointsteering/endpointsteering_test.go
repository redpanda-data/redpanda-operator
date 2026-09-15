// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package endpointsteering

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/portmapper"
	"github.com/stretchr/testify/require"

	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
)

func TestMapperConfig(t *testing.T) {
	_, err := mapperConfig(Options{})
	require.Error(t, err, "a resolver is what makes the checker able to recognise brokers at all")

	_, err = mapperConfig(Options{Resolver: Resolvers{}})
	require.Error(t, err, "an empty resolver set would drain every opted-in Service")

	resolver := ResolverFunc(func(context.Context, string, string) (*internalclient.ClusterBrokers, error) {
		return nil, ErrUnknownCluster
	})
	cfg, err := mapperConfig(Options{Resolver: Resolvers{resolver}, ClusterDomain: "cluster.local"})
	require.NoError(t, err)
	require.Equal(t, ManagedBy, cfg.ManagedBy)
	require.Equal(t, portmapper.AnnotationKey(ServiceAnnotation), cfg.ServiceKey)
	require.Equal(t, portmapper.LabelKey(PodGroupLabel), cfg.PodKey)
	require.Equal(t, DefaultResyncPeriod, cfg.ResyncPeriod)
	require.IsType(t, &Checker{}, cfg.Membership)
	require.Equal(t, "cluster.local", cfg.Membership.(*Checker).clusterDomain)

	// The library validates keys and the managed-by value; whatever we hand
	// it has to pass.
	_, err = portmapper.New(cfg)
	require.NoError(t, err)

	cfg, err = mapperConfig(Options{Resolver: resolver, ResyncPeriod: time.Minute})
	require.NoError(t, err)
	require.Equal(t, time.Minute, cfg.ResyncPeriod)
}
