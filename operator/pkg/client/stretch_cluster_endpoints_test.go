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
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// pod builds a running, addressable Pod labelled as a broker of the
// StretchCluster named "sc".
func pod(name string, mutate ...func(*corev1.Pod)) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "redpanda",
			Labels:    rendermulticluster.BrokerPodSelector("sc"),
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"},
	}
	for _, m := range mutate {
		m(p)
	}
	return p
}

// TestPodDialable pins which pods are excluded from the host list: only ones
// that cannot accept a connection. Readiness is deliberately not a factor.
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
			// The node lifecycle controller stamps this reason when the pod's
			// node stops reporting; the pod keeps its address but nothing
			// answers on it, so it must not be offered as an endpoint.
			name: "unready because its node is gone",
			pod: pod("broker-0", func(p *corev1.Pod) {
				p.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
					Reason: "NodeNotReady",
				}}
			}),
			want: false,
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

// TestPodDialability covers the lookup: a deleted pool's terminating pod, a
// pod without an address, and scoping to this cluster's brokers. Every broker
// pod appears in the map so callers can tell "exists but undialable" from
// "no pod at all".
func TestPodDialability(t *testing.T) {
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
		// An unrelated workload sharing the namespace must not be read at all.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "someone-elses-app-0", Namespace: "redpanda"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.8"},
		},
		// Nor must another StretchCluster's brokers in the same namespace.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-sc-pool-0-0",
				Namespace: "redpanda",
				Labels:    rendermulticluster.BrokerPodSelector("other-sc"),
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.7"},
		},
	).Build()

	got, err := podDialability(t.Context(), c, "redpanda", "sc")
	require.NoError(t, err, "a successful list must report the pod state as known")
	assert.Equal(t, map[string]bool{
		"cleanup-test-cleanup-pool-0-0": true,
		"cleanup-test-cleanup-pool-1-0": true,
		"cleanup-test-cleanup-pool-2-0": false,
		"config-sync-cfg-pool-0-0":      false,
	}, got)
}

// TestPodDialabilityListFailure pins the fallback: a failed pod list reports
// the state as unknown, and the caller then skips filtering instead of failing.
func TestPodDialabilityListFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			// Stands in for an unreachable or throttled apiserver.
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("apiserver unavailable")
			},
		}).Build()

	got, err := podDialability(t.Context(), c, "redpanda", "sc")
	require.Error(t, err, "a failed list must report the pod state as unknown")
	assert.Nil(t, got)
}

// stubCluster hands back a client; the nil embedded interface makes any
// unexercised method panic rather than return a zero value.
type stubCluster struct {
	cluster.Cluster
	c client.Client
}

func (s *stubCluster) GetClient() client.Client { return s.c }

// stubManager satisfies multicluster.Manager for the three methods
// stretchClusterEndpoints uses; same nil-embedding contract as stubCluster.
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

// TestStretchClusterEndpointsSkipsUndialablePods pins the fix end to end: a
// terminating pool's broker must not be offered to the admin client while
// enough dialable brokers remain.
func TestStretchClusterEndpointsSkipsUndialablePods(t *testing.T) {
	scheme := endpointsScheme(t)
	now := metav1.Now()

	// Cluster one's two brokers are up; cluster two's pool is being deleted.
	clusterOne := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-a", "sc"),
		pod("sc-pool-a-0"),
		brokerPool("pool-c", "sc"),
		pod("sc-pool-c-0"),
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
	assert.ElementsMatch(t, []string{"sc-pool-a-0.redpanda:9644", "sc-pool-c-0.redpanda:9644"}, endpoints,
		"the terminating pool's broker must not be offered to the admin client")
}

// TestStretchClusterEndpointsTwoHostFloor pins the floor end to end: with one
// dialable broker left, the list is padded back to two so rpadmin keeps
// leader resolution and try-every-host reads, preferring a pod-less endpoint
// (instant dialer rejection) over one whose pod holds a dead address.
func TestStretchClusterEndpointsTwoHostFloor(t *testing.T) {
	scheme := endpointsScheme(t)
	now := metav1.Now()

	// Cluster one: one broker up, one terminating (pod still present).
	clusterOne := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-a", "sc"),
		pod("sc-pool-a-0"),
		brokerPool("pool-b", "sc"),
		pod("sc-pool-b-0", func(p *corev1.Pod) {
			p.DeletionTimestamp = &now
			p.Finalizers = []string{"keep/for-fake-client"}
		}),
	).Build()

	// Cluster two: the pool is declared but its pod is gone entirely.
	clusterTwo := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		brokerPool("pool-c", "sc"),
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
	assert.Equal(t, []string{"sc-pool-a-0.redpanda:9644", "sc-pool-c-0.redpanda:9644"}, endpoints,
		"one dialable broker must be padded with the pod-less endpoint, not the terminating pod")
}

// TestPickAdminEndpoints pins the host-list decision table, the two-host
// floor included.
func TestPickAdminEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		declared, dialable, absent []string
		want                       []string
	}{
		{
			name:     "nothing dialable falls back to declared",
			declared: []string{"a", "b", "c"},
			want:     []string{"a", "b", "c"},
		},
		{
			name:     "two or more dialable stand alone",
			declared: []string{"a", "b", "c"},
			dialable: []string{"a", "b"},
			absent:   []string{"c"},
			want:     []string{"a", "b"},
		},
		{
			name:     "one dialable pads with an absent endpoint first",
			declared: []string{"a", "b", "c"},
			dialable: []string{"b"},
			absent:   []string{"c"},
			want:     []string{"b", "c"},
		},
		{
			name:     "one dialable without absent pads from declared",
			declared: []string{"a", "b"},
			dialable: []string{"b"},
			want:     []string{"b", "a"},
		},
		{
			name:     "a single declared broker cannot be padded",
			declared: []string{"a"},
			dialable: []string{"a"},
			want:     []string{"a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, pickAdminEndpoints(tc.declared, tc.dialable, tc.absent))
		})
	}
}

// TestStretchClusterEndpointsFallsBackWhenNoneDialable pins the safety valve:
// a whole-cluster outage must still produce a host list.
func TestStretchClusterEndpointsFallsBackWhenNoneDialable(t *testing.T) {
	scheme := endpointsScheme(t)

	// The cluster's only broker is mid-reschedule with no address assigned.
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
