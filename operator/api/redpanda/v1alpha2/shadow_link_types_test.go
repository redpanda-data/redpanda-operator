// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
)

func TestShadowLinkValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	baseLink := ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: ShadowLinkSpec{
			ShadowCluster: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "clusterOne",
				},
			},
			SourceCluster: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "clusterTwo",
				},
			},
		},
	}

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	for name, tt := range map[string]validationTestCase[*ShadowLink]{
		"no cluster source": {
			mutate: func(link *ShadowLink) {
				link.Spec.SourceCluster = nil
			},
			errors: []string{`spec.sourceCluster: required value`},
		},
		"no cluster target": {
			mutate: func(link *ShadowLink) {
				link.Spec.ShadowCluster = nil
			},
			errors: []string{`spec.shadowCluster: required value`},
		},
		"no interval when using timestamp": {
			mutate: func(link *ShadowLink) {
				link.Spec.TopicMetadataSyncOptions = &ShadowLinkTopicMetadataSyncOptions{
					StartOffset: ptr.To(TopicMetadataSyncOffsetTimestamp),
				}
			},
			errors: []string{`startoffsettimestamp must be specified when startoffset is set to timestamp`},
		},
		"no error when using timestamp": {
			mutate: func(link *ShadowLink) {
				link.Spec.TopicMetadataSyncOptions = &ShadowLinkTopicMetadataSyncOptions{
					StartOffset:          ptr.To(TopicMetadataSyncOffsetTimestamp),
					StartOffsetTimestamp: ptr.To(metav1.Now()),
				}
			},
		},
		"error on changing shadow cluster clusterRef": {
			mutateUpdate: func(link *ShadowLink) {
				link.Spec.ShadowCluster = &ClusterSource{
					ClusterRef: &ClusterRef{
						Name: "different-cluster",
					},
				}
			},
			updateErrors: []string{`ClusterSource is immutable`},
		},
		"error on changing source cluster clusterRef": {
			mutateUpdate: func(link *ShadowLink) {
				link.Spec.SourceCluster = &ClusterSource{
					ClusterRef: &ClusterRef{
						Name: "different-cluster",
					},
				}
			},
			updateErrors: []string{`ClusterSource clusterRef is immutable`},
		},
		"error on setting no static kafka brokers": {
			mutate: func(link *ShadowLink) {
				link.Spec.SourceCluster = &ClusterSource{
					StaticConfiguration: &StaticConfigurationSource{
						Kafka: &KafkaAPISpec{
							Brokers: []string{},
						},
					},
				}
			},
			errors: []string{`should have at least 1 item`},
		},
		"error on not setting static kafka block": {
			mutate: func(link *ShadowLink) {
				link.Spec.SourceCluster = &ClusterSource{
					StaticConfiguration: &StaticConfigurationSource{},
				}
			},
			errors: []string{`static configuration must contain a kafka block`},
		},
		"error on schema registry api options without a source url": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode:                    ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{},
				}
			},
			errors: []string{`spec.schemaRegistrySyncOptions.shadowSchemaRegistryAPI.sourceURL in body should be at least 1 chars long`},
		},
		"error on api mode without schema registry api options": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
				}
			},
			errors: []string{`shadowSchemaRegistryAPI is required when schema_registry_shadowing_mode is api`},
		},
		"error on schema registry api options outside api mode": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeTopic,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://registry.example.com",
					},
				}
			},
			errors: []string{`shadowSchemaRegistryAPI may only be set when schema_registry_shadowing_mode is api`},
		},
		"error on schema registry tls with insecureSkipTlsVerify": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://registry.example.com",
						TLS:       &CommonTLS{InsecureSkipTLSVerify: true},
					},
				}
			},
			errors: []string{`insecureSkipTlsVerify is not supported for schema registry connections`},
		},
		"error on schema registry tls with deprecated secret references": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://registry.example.com",
						TLS:       &CommonTLS{DeprecatedCaCert: &SecretKeyRef{Name: "ca"}},
					},
				}
			},
			errors: []string{`use caCert, cert, and key rather than the deprecated secret-reference fields`},
		},
		"error on destination with both identity and exact": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://registry.example.com",
						Destination: &ShadowLinkSchemaRegistryContextDestination{
							Identity: &ShadowLinkSchemaRegistryIdentityContextMapping{},
							Exact: []ShadowLinkSchemaRegistryContextMapping{{
								Source:      ".",
								Destination: ".shadow",
							}},
						},
					},
				}
			},
			errors: []string{`exactly one of identity or exact must be set`},
		},
		"error on empty destination": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL:   "https://registry.example.com",
						Destination: &ShadowLinkSchemaRegistryContextDestination{},
					},
				}
			},
			errors: []string{`exactly one of identity or exact must be set`},
		},
		"error on schema registry basic auth without a value source": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://registry.example.com",
						Authentication: &ShadowLinkSchemaRegistryAuthentication{
							Basic: &ShadowLinkSchemaRegistryBasicAuthentication{
								Username: ValueSource{},
								Password: ValueSource{},
							},
						},
					},
				}
			},
			errors: []string{`one of inline, configmapkeyref, secretkeyref, or externalsecretref must be set`},
		},
		"no errors on schema registry api options with secret references": {
			mutate: func(link *ShadowLink) {
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeAPI,
					ShadowSchemaRegistryAPI: &ShadowLinkSchemaRegistryAPIOptions{
						SourceURL: "https://psrc-xxxxx.us-east-1.aws.confluent.cloud",
						Authentication: &ShadowLinkSchemaRegistryAuthentication{
							Basic: &ShadowLinkSchemaRegistryBasicAuthentication{
								Username: ValueSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "sr-credentials"},
										Key:                  "api-key",
									},
								},
								Password: ValueSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "sr-credentials"},
										Key:                  "api-secret",
									},
								},
							},
						},
					},
				}
			},
		},
		"no errors on disabling role and schema replication": {
			mutate: func(link *ShadowLink) {
				link.Spec.RoleSyncOptions = &ShadowLinkRoleSyncOptions{
					Enabled: ptr.To(false),
				}
				link.Spec.SchemaRegistrySyncOptions = &ShadowLinkSchemaRegistrySyncOptions{
					Mode: ShadowLinkSchemaRegistrySyncOptionsModeDisabled,
				}
			},
		},
		"no errors on update when using SASL on static config": {
			doUpdate: true,
			rawManifest: `
apiVersion: cluster.redpanda.com/v1alpha2
kind: ShadowLink
metadata:
  namespace: default
spec:
  shadowCluster:
    clusterRef:
      name: bar
  sourceCluster:
    staticConfiguration:
      kafka:
        brokers:
        - foo:9093
        sasl:
          username: user
          mechanism: scram-sha-512
          passwordSecretRef:
            name: sasl-password
            key: password`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			runValidationTest(ctx, t, tt, c, &baseLink)
		})
	}
}

func TestShadowLinkDefaults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	require.NoError(t, c.Create(ctx, &ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: ShadowLinkSpec{
			ShadowCluster: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "clusterOne",
				},
			},
			SourceCluster: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "clusterTwo",
				},
			},
			TopicMetadataSyncOptions: &ShadowLinkTopicMetadataSyncOptions{},
		},
	}))

	var link ShadowLink
	require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: "name"}, &link))

	require.Len(t, link.Status.Conditions, 1)
	require.Equal(t, ResourceConditionTypeSynced, link.Status.Conditions[0].Type)
	require.Equal(t, metav1.ConditionUnknown, link.Status.Conditions[0].Status)
	require.Equal(t, ResourceConditionReasonPending, link.Status.Conditions[0].Reason)

	require.NotNil(t, link.Spec.TopicMetadataSyncOptions)
	require.Equal(t, 30*time.Second, link.Spec.TopicMetadataSyncOptions.Interval.Duration)
	require.NotNil(t, link.Spec.TopicMetadataSyncOptions.StartOffset)
	require.Equal(t, TopicMetadataSyncOffsetEarliest, *link.Spec.TopicMetadataSyncOptions.StartOffset)

	// RBAC roles replicate by default with an include-all filter.
	require.NotNil(t, link.Spec.RoleSyncOptions)
	require.NotNil(t, link.Spec.RoleSyncOptions.Enabled)
	require.True(t, *link.Spec.RoleSyncOptions.Enabled)
	require.Equal(t, 30*time.Second, link.Spec.RoleSyncOptions.Interval.Duration)
	require.Equal(t, []NameFilter{{
		Name:        "*",
		FilterType:  FilterTypeInclude,
		PatternType: PatternTypeLiteral,
	}}, link.Spec.RoleSyncOptions.RoleNameFilters)

	// Schemas replicate in topic mode by default.
	require.NotNil(t, link.Spec.SchemaRegistrySyncOptions)
	require.Equal(t, ShadowLinkSchemaRegistrySyncOptionsModeTopic, link.Spec.SchemaRegistrySyncOptions.Mode)
	require.Nil(t, link.Spec.SchemaRegistrySyncOptions.ShadowSchemaRegistryAPI)
}
