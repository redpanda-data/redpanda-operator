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
	"slices"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// pod builds a Pod in the shape the dialability check reads.
func pod(name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "redpanda"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

// TestPodDialable pins which pods are excluded from the admin client's host
// list. Only pods that cannot accept a connection are: readiness is
// deliberately not a factor, because the operator reads a rejoining broker's
// identity from an unready pod on purpose.
func TestPodDialable(t *testing.T) {
	for _, tc := range []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "running with an address",
			pod:  pod("broker-0"),
			want: true,
		},
		{
			name: "unready but addressable is still dialable",
			pod: pod("broker-0", func(p *corev1.Pod) {
				p.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				}}
			}),
			want: true,
		},
		{
			name: "terminating",
			pod: pod("broker-0", func(p *corev1.Pod) {
				now := metav1.Now()
				p.DeletionTimestamp = &now
				p.Finalizers = []string{"keep/for-fake-client"}
			}),
			want: false,
		},
		{
			name: "unscheduled, no address yet",
			pod: pod("broker-0", func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodPending
				p.Status.PodIP = ""
			}),
			want: false,
		},
		{
			name: "succeeded",
			pod: pod("broker-0", func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodSucceeded
			}),
			want: false,
		},
		{
			name: "failed",
			pod: pod("broker-0", func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodFailed
			}),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, podDialable(tc.pod))
		})
	}
}

// TestDialablePodNames covers the lookup that stretchClusterEndpoints filters
// against, including the case that motivated it: a broker pool being deleted
// leaves a terminating pod that must not be offered to the admin client, since
// the decommission that removes its StatefulSet is itself gated on a
// cluster-health read.
func TestDialablePodNames(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	now := metav1.Now()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		pod("cleanup-test-cleanup-pool-0-0"),
		pod("cleanup-test-cleanup-pool-1-0"),
		// The pool under deletion.
		pod("cleanup-test-cleanup-pool-2-0", func(p *corev1.Pod) {
			p.DeletionTimestamp = &now
			p.Finalizers = []string{"keep/for-fake-client"}
		}),
		// Mid-roll, rescheduled and not yet assigned an address.
		pod("config-sync-cfg-pool-0-0", func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodPending
			p.Status.PodIP = ""
		}),
		// A pod in another namespace must not leak in.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "elsewhere-0", Namespace: "other"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.9"},
		},
	).Build()

	got, err := dialablePodNames(t.Context(), c, "redpanda")
	require.NoError(t, err, "a successful list must report the pod state as known")
	assert.Equal(t, map[string]bool{
		"cleanup-test-cleanup-pool-0-0": true,
		"cleanup-test-cleanup-pool-1-0": true,
	}, got)
}

// TestDialablePodNamesListFailure pins the fallback: losing the pod list must
// cost only the filter. Reporting the state as unknown makes
// stretchClusterEndpoints offer its endpoints unfiltered, which is how the
// admin client behaved before the filter existed, rather than failing a
// construction that never needed pod state.
func TestDialablePodNamesListFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			// Stands in for an unreachable or throttled apiserver.
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("apiserver unavailable")
			},
		}).Build()

	got, err := dialablePodNames(t.Context(), c, "redpanda")
	require.Error(t, err, "a failed list must report the pod state as unknown")
	assert.Nil(t, got)
}

// stubCluster satisfies cluster.Cluster well enough to hand back a client. The
// embedded interface is nil: any method this test does not exercise panics
// rather than silently returning a zero value.
type stubCluster struct {
	cluster.Cluster
	c client.Client
}

func (s *stubCluster) GetClient() client.Client { return s.c }

// stubManager satisfies multicluster.Manager for the three methods
// stretchClusterEndpoints reaches through. Same nil-embedding contract as
// stubCluster.
type stubManager struct {
	multicluster.Manager
	clients map[string]client.Client
}

func (m *stubManager) GetClusterNames() []string {
	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func (m *stubManager) IsClusterReachable(string) bool { return true }

func (m *stubManager) GetCluster(_ context.Context, name string) (cluster.Cluster, error) {
	c, ok := m.clients[name]
	if !ok {
		return nil, errors.Newf("no cluster %q", name)
	}
	return &stubCluster{c: c}, nil
}

// brokerPool builds a RedpandaBrokerPool bound to sc with a single replica.
func brokerPool(name, scName string) *redpandav1alpha2.RedpandaBrokerPool {
	return &redpandav1alpha2.RedpandaBrokerPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "redpanda"},
		Spec: redpandav1alpha2.BrokerPoolSpec{
			EmbeddedBrokerPoolSpec: redpandav1alpha2.EmbeddedBrokerPoolSpec{
				Replicas: ptr.To(int32(1)),
			},
			ClusterRef: redpandav1alpha2.ClusterRef{
				Kind: ptr.To(redpandav1alpha2.StretchClusterRefKind),
				Name: scName,
			},
		},
	}
}

func endpointsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))
	return scheme
}

// TestStretchClusterEndpointsSkipsUndialablePods is the test that pins the fix
// itself: the endpoint list is derived from each pool's declared replica count,
// so without the pod filter it offers a broker that is provably absent, and the
// steps gating on it (decommission's cluster-health read, the maintenance-mode
// broker list) then fail on whichever host they happen to pick.
func TestStretchClusterEndpointsSkipsUndialablePods(t *testing.T) {
	scheme := endpointsScheme(t)
	now := metav1.Now()

	// Cluster one's broker is up; cluster two's pool is being deleted, which is
	// exactly the SingleBrokerPoolDeletion shape.
	clusterOne := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-a", "sc"),
		pod("sc-pool-a-0"),
	).Build()

	clusterTwo := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-b", "sc"),
		pod("sc-pool-b-0", func(p *corev1.Pod) {
			p.DeletionTimestamp = &now
			p.Finalizers = []string{"keep/for-fake-client"}
		}),
	).Build()

	c := &Factory{mgr: &stubManager{clients: map[string]client.Client{
		"cluster-1": clusterOne,
		"cluster-2": clusterTwo,
	}}}

	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "sc", Namespace: "redpanda"},
	}

	endpoints, err := c.stretchClusterEndpoints(t.Context(), sc, 9644)
	require.NoError(t, err)
	assert.Equal(t, []string{"sc-pool-a-0.redpanda:9644"}, endpoints,
		"the terminating pool's broker must not be offered to the admin client")
}

// TestStretchClusterEndpointsFallsBackWhenNoneDialable pins the safety valve. A
// whole-cluster outage must still produce a host list: the caller treats an
// empty one as fatal, and failing there would trade a dial error for a
// different, less informative one.
func TestStretchClusterEndpointsFallsBackWhenNoneDialable(t *testing.T) {
	scheme := endpointsScheme(t)

	// Both brokers are mid-reschedule with no address assigned.
	pending := func(name string) *corev1.Pod {
		return pod(name, func(p *corev1.Pod) {
			p.Status.Phase = corev1.PodPending
			p.Status.PodIP = ""
		})
	}

	clusterOne := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-a", "sc"), pending("sc-pool-a-0"),
	).Build()

	c := &Factory{mgr: &stubManager{clients: map[string]client.Client{"cluster-1": clusterOne}}}

	sc := &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "sc", Namespace: "redpanda"},
	}

	endpoints, err := c.stretchClusterEndpoints(t.Context(), sc, 9644)
	require.NoError(t, err)
	assert.Equal(t, []string{"sc-pool-a-0.redpanda:9644"}, endpoints,
		"with nothing dialable the declared list must still be returned")
}
