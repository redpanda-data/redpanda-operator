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

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

func TestResolvers(t *testing.T) {
	brokers := &fakeCluster{podSelector: map[string]string{"app.kubernetes.io/instance": testCluster}}
	boom := errors.New("boom")

	unknown := ResolverFunc(func(context.Context, string, string) (Cluster, error) {
		return nil, ErrUnknownCluster
	})
	knows := ResolverFunc(func(context.Context, string, string) (Cluster, error) {
		return brokers, nil
	})
	failing := ResolverFunc(func(context.Context, string, string) (Cluster, error) {
		return nil, boom
	})
	unreachable := ResolverFunc(func(context.Context, string, string) (Cluster, error) {
		t.Fatal("a resolver after a definitive answer must not be consulted")
		return nil, nil
	})

	for _, tc := range []struct {
		name      string
		resolvers Resolvers
		want      Cluster
		wantErr   error
	}{
		{name: "no resolvers", wantErr: ErrUnknownCluster},
		{name: "nobody knows", resolvers: Resolvers{unknown, unknown}, wantErr: ErrUnknownCluster},
		{name: "later resolver knows", resolvers: Resolvers{unknown, knows, unreachable}, want: brokers},
		{name: "failure is definitive", resolvers: Resolvers{failing, unreachable}, wantErr: boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.resolvers.Resolve(t.Context(), testNamespace, testCluster)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Same(t, tc.want, got)
		})
	}
}

// recordingFactory stands in for client.Factory, recording the cluster
// object it was asked about. Its ClusterBrokers is shaped like the real
// one's -- a concrete type, adapted by [BrokersOf] -- so the adapter is
// exercised too.
type recordingFactory struct {
	seen    []any
	brokers *fakeCluster
}

func (f *recordingFactory) ClusterBrokers(_ context.Context, obj any, _ string) (*fakeCluster, error) {
	f.seen = append(f.seen, obj)
	return f.brokers, nil
}

func TestTypedResolvers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.Install(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))

	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&redpandav1alpha2.Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "v2", Namespace: testNamespace}},
		&vectorizedv1alpha1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "v1", Namespace: testNamespace}},
		&redpandav1alpha2.StretchCluster{ObjectMeta: metav1.ObjectMeta{Name: "stretch", Namespace: testNamespace}},
	).Build()

	for _, tc := range []struct {
		name     string
		resolver func(client.Reader, BrokersFunc) Resolver
		group    string
		wantKind any
	}{
		{name: "v2", resolver: V2Resolver, group: "v2", wantKind: &redpandav1alpha2.Redpanda{}},
		{name: "v1", resolver: V1Resolver, group: "v1", wantKind: &vectorizedv1alpha1.Cluster{}},
		{name: "stretch", resolver: StretchResolver, group: "stretch", wantKind: &redpandav1alpha2.StretchCluster{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := &recordingFactory{brokers: &fakeCluster{podSelector: map[string]string{"app.kubernetes.io/instance": tc.group}}}
			resolver := tc.resolver(reader, BrokersOf(factory.ClusterBrokers))

			got, err := resolver.Resolve(t.Context(), testNamespace, tc.group)
			require.NoError(t, err)
			require.Same(t, factory.brokers, got)
			require.Len(t, factory.seen, 1)
			require.IsType(t, tc.wantKind, factory.seen[0])
			require.Equal(t, tc.group, factory.seen[0].(client.Object).GetName())

			// The other kinds' clusters are not this resolver's business, and
			// neither is a name that exists nowhere.
			for _, other := range []string{"v2", "v1", "stretch", "missing"} {
				if other == tc.group {
					continue
				}
				_, err := resolver.Resolve(t.Context(), testNamespace, other)
				require.ErrorIs(t, err, ErrUnknownCluster, "group %q", other)
			}
			require.Len(t, factory.seen, 1, "unknown groups never reach the factory")
		})
	}
}
