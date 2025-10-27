// Copyright 2025 Redpanda Data, Inc.
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
}
