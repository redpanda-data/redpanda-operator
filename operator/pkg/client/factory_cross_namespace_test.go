// Copyright 2026 Redpanda Data, Inc.
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
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

type fakeCluster struct {
	cluster.Cluster
	client client.Client
}

func (f *fakeCluster) GetClient() client.Client { return f.client }

type fakeManager struct {
	multicluster.Manager
	cluster cluster.Cluster
}

func (f *fakeManager) GetCluster(_ context.Context, _ string) (cluster.Cluster, error) {
	return f.cluster, nil
}

func (f *fakeManager) GetLogger() logr.Logger { return logr.Discard() }

func (f *fakeManager) GetLeader() string           { return "" }
func (f *fakeManager) GetClusterNames() []string   { return []string{""} }
func (f *fakeManager) GetLocalClusterName() string { return "" }
func (f *fakeManager) AddOrReplaceCluster(context.Context, string, cluster.Cluster) error {
	return nil
}
func (f *fakeManager) Health(*http.Request) error { return nil }

func newCrossNamespaceFactory(t *testing.T, objects ...client.Object) *Factory {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.Install(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return &Factory{
		mgr: &fakeManager{
			cluster: &fakeCluster{client: cl},
		},
	}
}

func TestFactoryGetV2ClusterUsesClusterRefNamespace(t *testing.T) {
	factory := newCrossNamespaceFactory(t, &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "redpanda",
			Namespace: "cluster-ns",
		},
	})

	cluster, err := factory.getV2Cluster(t.Context(), &redpandav1alpha2.Console{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "console",
			Namespace: "console-ns",
		},
		Spec: redpandav1alpha2.ConsoleSpec{
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name:      "redpanda",
					Namespace: ptr.To("cluster-ns"),
				},
			},
		},
	}, "")
	require.NoError(t, err)
	require.NotNil(t, cluster)
	require.Equal(t, "cluster-ns", cluster.Namespace)
}

func TestFactoryGetRemoteV2ClusterUsesClusterRefNamespace(t *testing.T) {
	factory := newCrossNamespaceFactory(t, &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "source-redpanda",
			Namespace: "source-ns",
		},
	})

	cluster, err := factory.getRemoteV2Cluster(t.Context(), &redpandav1alpha2.ShadowLink{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shadow-link",
			Namespace: "consumer-ns",
		},
		Spec: redpandav1alpha2.ShadowLinkSpec{
			SourceCluster: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name:      "source-redpanda",
					Namespace: ptr.To("source-ns"),
				},
			},
		},
	}, "")
	require.NoError(t, err)
	require.NotNil(t, cluster)
	require.Equal(t, "source-ns", cluster.Namespace)
}
