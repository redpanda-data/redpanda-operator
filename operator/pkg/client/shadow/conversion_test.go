// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package shadow

import (
	"testing"

	"github.com/stretchr/testify/require"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestConvertClientOptions verifies that ShadowLink.spec.clientOptions is
// forwarded into the admin v2 ShadowLinkClientOptions request that the syncer
// sends to Redpanda. This is the operator half of the "k8s configures the
// shadow link on RP" path; the broker resolves a 0 to its own default (see
// converter.cc / connection_config in redpanda), so an unset block must remain
// a no-op that carries the existing bootstrap/TLS/auth settings unchanged.
func TestConvertClientOptions(t *testing.T) {
	remote := RemoteClusterSettings{
		BootstrapServers: []string{"source-broker:9092"},
	}

	link := func(opts *redpandav1alpha2.ShadowLinkClientOptions) *redpandav1alpha2.ShadowLink {
		return &redpandav1alpha2.ShadowLink{Spec: redpandav1alpha2.ShadowLinkSpec{ClientOptions: opts}}
	}

	t.Run("set values are forwarded", func(t *testing.T) {
		got := convertCRDToAPIShadowLink(link(&redpandav1alpha2.ShadowLinkClientOptions{
			FetchMinBytes:          1,
			FetchWaitMaxMs:         50,
			FetchMaxBytes:          1048576,
			FetchPartitionMaxBytes: 524288,
			MetadataMaxAgeMs:       5000,
			ConnectionTimeoutMs:    2000,
			RetryBackoffMs:         250,
		}), remote)

		co := got.GetConfigurations().GetClientOptions()
		require.Equal(t, int32(1), co.GetFetchMinBytes())
		require.Equal(t, int32(50), co.GetFetchWaitMaxMs())
		require.Equal(t, int32(1048576), co.GetFetchMaxBytes())
		require.Equal(t, int32(524288), co.GetFetchPartitionMaxBytes())
		require.Equal(t, int32(5000), co.GetMetadataMaxAgeMs())
		require.Equal(t, int32(2000), co.GetConnectionTimeoutMs())
		require.Equal(t, int32(250), co.GetRetryBackoffMs())
		// Connection identity is still derived from the cluster sources.
		require.Equal(t, []string{"source-broker:9092"}, co.GetBootstrapServers())
	})

	t.Run("unset block is a zero-passthrough no-op", func(t *testing.T) {
		got := convertCRDToAPIShadowLink(link(nil), remote)

		co := got.GetConfigurations().GetClientOptions()
		// All 0 => the broker applies its own defaults, so existing links are
		// unaffected by this feature.
		require.Zero(t, co.GetFetchMinBytes())
		require.Zero(t, co.GetFetchWaitMaxMs())
		require.Zero(t, co.GetFetchMaxBytes())
		require.Zero(t, co.GetFetchPartitionMaxBytes())
		require.Zero(t, co.GetMetadataMaxAgeMs())
		require.Zero(t, co.GetConnectionTimeoutMs())
		require.Zero(t, co.GetRetryBackoffMs())
		require.Equal(t, []string{"source-broker:9092"}, co.GetBootstrapServers())
	})
}
