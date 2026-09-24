// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// TestDefaultPhase pins the one-way gate out of Provisioning: once a broker
// has registered (Status.BrokerID set), a pod readiness dip must not regress
// its phase — the readiness probe is cluster-scoped, so one restarting
// broker flips every pod unready and would otherwise flap every registered
// broker's phase Running→Provisioning.
func TestDefaultPhase(t *testing.T) {
	registered := &redpandav1alpha2.Broker{}
	registered.Status.BrokerID = ptr.To(int32(4))
	unregistered := &redpandav1alpha2.Broker{}
	diskLost := &redpandav1alpha2.Broker{}
	diskLost.Status.BrokerID = ptr.To(int32(4))
	diskLost.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{}

	readyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}}}
	unreadyPod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodReady, Status: corev1.ConditionFalse},
	}}}
	unschedulablePod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
		{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
	}}}

	for name, tc := range map[string]struct {
		broker *redpandav1alpha2.Broker
		pod    *corev1.Pod
		want   redpandav1alpha2.BrokerPhase
	}{
		"unregistered, pod not ready":  {unregistered, unreadyPod, redpandav1alpha2.BrokerPhaseProvisioning},
		"unregistered, pod ready":      {unregistered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, pod ready":        {registered, readyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, readiness dip":    {registered, unreadyPod, redpandav1alpha2.BrokerPhaseRunning},
		"registered, no pod yet":       {registered, &corev1.Pod{}, redpandav1alpha2.BrokerPhaseRunning},
		"stuck overrides registration": {registered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
		"stuck overrides provisioning": {unregistered, unschedulablePod, redpandav1alpha2.BrokerPhaseStuck},
		"disk-lost latch is sticky":    {diskLost, readyPod, redpandav1alpha2.BrokerPhaseDiskLost},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, defaultPhase(tc.broker, tc.pod))
		})
	}
}

// TestReconcileUnmanagedIsNoOp pins the managed=false gate: the only write is
// dropping the finalizer (so deletion while unmanaged needs no operator), and
// after that the Broker quiesces — no pod is created for a podless Broker and
// a repeat pass writes nothing.
func TestReconcileUnmanagedIsNoOp(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rp-abcde",
			Namespace:   "ns",
			Annotations: map[string]string{feature.V2Managed.Key: "false"},
			Finalizers:  []string{brokerFinalizerName},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "rp"},
			NetworkIndex: ptr.To(int32(0)),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(broker).
		WithStatusSubresource(&redpandav1alpha2.Broker{}).Build()
	r := &BrokerReconciler{Manager: &unmanagedTestManager{cluster: &unmanagedTestCluster{client: c}}}

	req := mcreconcile.Request{Request: reconcile.Request{NamespacedName: client.ObjectKeyFromObject(broker)}}
	result, err := r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.IsZero())

	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(broker), broker))
	require.Empty(t, broker.Finalizers)

	var pods corev1.PodList
	require.NoError(t, c.List(t.Context(), &pods))
	require.Empty(t, pods.Items)

	rv := broker.ResourceVersion
	result, err = r.Reconcile(t.Context(), req)
	require.NoError(t, err)
	require.True(t, result.IsZero())
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(broker), broker))
	require.Equal(t, rv, broker.ResourceVersion)
}

// unmanagedTestManager/-Cluster satisfy just enough of multicluster.Manager
// and cluster.Cluster to drive Reconcile against a fake client.
type unmanagedTestManager struct {
	multicluster.Manager
	cluster cluster.Cluster
}

func (m *unmanagedTestManager) GetCluster(context.Context, string) (cluster.Cluster, error) {
	return m.cluster, nil
}

type unmanagedTestCluster struct {
	cluster.Cluster
	client client.Client
}

func (c *unmanagedTestCluster) GetClient() client.Client { return c.client }
