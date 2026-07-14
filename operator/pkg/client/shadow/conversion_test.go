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
	"time"

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

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

func TestConvertRoleSyncOptions(t *testing.T) {
	includeAll := func(options *adminv2api.RoleSyncOptions) {
		t.Helper()
		require.Len(t, options.RoleNameFilters, 1)
		require.Equal(t, "*", options.RoleNameFilters[0].Name)
		require.Equal(t, adminv2api.FilterType_FILTER_TYPE_INCLUDE, options.RoleNameFilters[0].FilterType)
		require.Equal(t, adminv2api.PatternType_PATTERN_TYPE_LITERAL, options.RoleNameFilters[0].PatternType)
	}

	t.Run("nil options default to replicating all roles", func(t *testing.T) {
		options := convertCRDToAPIShadowLinkRoleSyncOptions(nil)
		require.NotNil(t, options)
		require.False(t, options.Paused)
		includeAll(options)
	})

	t.Run("empty filters default to include-all", func(t *testing.T) {
		options := convertCRDToAPIShadowLinkRoleSyncOptions(&redpandav1alpha2.ShadowLinkRoleSyncOptions{
			Interval: ptr.To(metav1.Duration{Duration: time.Minute}),
			Paused:   true,
		})
		require.NotNil(t, options)
		require.True(t, options.Paused)
		require.Equal(t, time.Minute, options.Interval.AsDuration())
		includeAll(options)
	})

	t.Run("enabled=false turns role replication off", func(t *testing.T) {
		require.Nil(t, convertCRDToAPIShadowLinkRoleSyncOptions(&redpandav1alpha2.ShadowLinkRoleSyncOptions{
			Enabled: ptr.To(false),
		}))
	})

	t.Run("explicit filters are passed through", func(t *testing.T) {
		options := convertCRDToAPIShadowLinkRoleSyncOptions(&redpandav1alpha2.ShadowLinkRoleSyncOptions{
			Enabled: ptr.To(true),
			RoleNameFilters: []redpandav1alpha2.NameFilter{{
				Name:        "admin-",
				FilterType:  redpandav1alpha2.FilterTypeExclude,
				PatternType: redpandav1alpha2.PatternTypePrefixed,
			}},
		})
		require.NotNil(t, options)
		require.Len(t, options.RoleNameFilters, 1)
		require.Equal(t, "admin-", options.RoleNameFilters[0].Name)
		require.Equal(t, adminv2api.FilterType_FILTER_TYPE_EXCLUDE, options.RoleNameFilters[0].FilterType)
		require.Equal(t, adminv2api.PatternType_PATTERN_TYPE_PREFIX, options.RoleNameFilters[0].PatternType)
	})
}

func TestConvertSchemaRegistrySyncOptions(t *testing.T) {
	t.Run("nil options default to topic mode", func(t *testing.T) {
		options := convertCRDToAPISchemaRegistrySyncOptions(nil, nil)
		require.NotNil(t, options)
		require.NotNil(t, options.GetShadowSchemaRegistryTopic())
		require.Nil(t, options.GetShadowSchemaRegistryApi())
	})

	t.Run("legacy topic mode still maps to topic mode", func(t *testing.T) {
		options := convertCRDToAPISchemaRegistrySyncOptions(&redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions{
			Mode: redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptionsModeTopic,
		}, nil)
		require.NotNil(t, options)
		require.NotNil(t, options.GetShadowSchemaRegistryTopic())
	})

	t.Run("enabled=false turns schema replication off", func(t *testing.T) {
		require.Nil(t, convertCRDToAPISchemaRegistrySyncOptions(&redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions{
			Enabled: ptr.To(false),
		}, nil))
	})

	t.Run("api mode maps the full configuration", func(t *testing.T) {
		options := convertCRDToAPISchemaRegistrySyncOptions(&redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions{
			ShadowSchemaRegistryAPI: &redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
				SourceURL:                  "https://psrc-xxxxx.us-east-1.aws.confluent.cloud",
				TailInterval:               ptr.To(metav1.Duration{Duration: 10 * time.Second}),
				FullSyncInterval:           ptr.To(metav1.Duration{Duration: 5 * time.Minute}),
				MaxSourceRequestsPerSecond: ptr.To(int32(30)),
				SourceFilter: &redpandav1alpha2.ShadowLinkSchemaRegistrySourceFilter{
					Contexts: []string{"."},
					Subjects: []string{"orders-value"},
				},
				ContextMappings: []redpandav1alpha2.ShadowLinkSchemaRegistryContextMapping{{
					Source:      ".",
					Destination: ".shadow",
				}},
				UnsupportedSchemaFeaturePolicy: ptr.To(redpandav1alpha2.UnsupportedSchemaFeaturePolicyRemove),
			},
		}, &SchemaRegistrySettings{
			BasicAuthentication: &HTTPBasicAuthenticationSettings{
				Username: "api-key",
				Password: "api-secret",
			},
			TLSSettings: &TLSSettings{CA: "ca-pem"},
		})
		require.NotNil(t, options)
		require.Nil(t, options.GetShadowSchemaRegistryTopic())

		api := options.GetShadowSchemaRegistryApi()
		require.NotNil(t, api)
		require.Equal(t, "https://psrc-xxxxx.us-east-1.aws.confluent.cloud", api.SourceUrl)
		require.Equal(t, 10*time.Second, api.TailInterval.AsDuration())
		require.Equal(t, 5*time.Minute, api.FullSyncInterval.AsDuration())
		require.Equal(t, int32(30), api.MaxSourceRequestsPerSecond)
		require.Equal(t, []string{"."}, api.SourceFilter.Contexts)
		require.Equal(t, []string{"orders-value"}, api.SourceFilter.Subjects)
		require.Equal(t, adminv2api.UnsupportedSchemaFeaturePolicy_UNSUPPORTED_SCHEMA_FEATURE_POLICY_REMOVE, api.UnsupportedSchemaFeaturePolicy)

		exact := api.Destination.GetExact()
		require.NotNil(t, exact)
		require.Len(t, exact.Mappings, 1)
		require.Equal(t, ".", exact.Mappings[0].Source)
		require.Equal(t, ".shadow", exact.Mappings[0].Destination)

		basic := api.AuthOptions.GetBasic()
		require.NotNil(t, basic)
		require.Equal(t, "api-key", basic.Username)
		require.Equal(t, "api-secret", basic.Password)
		require.True(t, basic.PasswordSet)

		require.NotNil(t, api.TlsSettings)
		require.True(t, api.TlsSettings.Enabled)
		require.Equal(t, "ca-pem", api.TlsSettings.GetTlsPemSettings().Ca)
	})

	t.Run("api mode without mappings keeps identity destination", func(t *testing.T) {
		options := convertCRDToAPISchemaRegistrySyncOptions(&redpandav1alpha2.ShadowLinkSchemaRegistrySyncOptions{
			ShadowSchemaRegistryAPI: &redpandav1alpha2.ShadowLinkSchemaRegistryAPIOptions{
				SourceURL: "https://registry.example.com",
			},
		}, nil)
		api := options.GetShadowSchemaRegistryApi()
		require.NotNil(t, api)
		require.NotNil(t, api.Destination.GetIdentity())
		require.Nil(t, api.Destination.GetExact())
		require.Nil(t, api.AuthOptions)
		require.Nil(t, api.TlsSettings)
	})
}

func TestConvertShadowLinkConfigurationDefaults(t *testing.T) {
	link := &redpandav1alpha2.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{Name: "link", Namespace: metav1.NamespaceDefault},
	}

	configurations := convertCRDToAPIShadowLinkConfiguration(link, RemoteClusterSettings{
		BootstrapServers: []string{"broker:9092"},
	})

	// Roles and schemas replicate by default even when the spec sections
	// are entirely absent.
	require.NotNil(t, configurations.RoleSyncOptions)
	require.Len(t, configurations.RoleSyncOptions.RoleNameFilters, 1)
	require.NotNil(t, configurations.SchemaRegistrySyncOptions)
	require.NotNil(t, configurations.SchemaRegistrySyncOptions.GetShadowSchemaRegistryTopic())
}
