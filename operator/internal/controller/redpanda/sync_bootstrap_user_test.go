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
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// mockCluster implements cluster.Cluster backed by a fake client.
type mockCluster struct {
	cluster.Cluster
	client client.Client
}

func (m *mockCluster) GetClient() client.Client { return m.client }

// mockManager implements multicluster.Manager with fake clusters for testing.
type mockManager struct {
	multicluster.Manager
	clusters map[string]*mockCluster
	names    []string
}

func (m *mockManager) GetClusterNames() []string { return m.names }
func (m *mockManager) GetCluster(_ context.Context, name string) (cluster.Cluster, error) {
	cl, ok := m.clusters[name]
	if !ok {
		return nil, k8sapierrors.NewNotFound(corev1.Resource("cluster"), name)
	}
	return cl, nil
}
func (m *mockManager) GetLeader() string           { return m.names[0] }
func (m *mockManager) GetLocalClusterName() string { return m.names[0] }
func (m *mockManager) AddOrReplaceCluster(_ context.Context, _ string, _ cluster.Cluster) error {
	return nil
}
func (m *mockManager) Health(_ *http.Request) error     { return nil }
func (m *mockManager) IsClusterReachable(_ string) bool { return true }
func (m *mockManager) GetLogger() logr.Logger           { return logr.Discard() }

func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = redpandav1alpha2.Install(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func newMockManager(clusterNames []string, clients map[string]client.Client) *mockManager {
	clusters := map[string]*mockCluster{}
	for name, c := range clients {
		clusters[name] = &mockCluster{client: c}
	}
	return &mockManager{clusters: clusters, names: clusterNames}
}

func newTestState(sc *redpandav1alpha2.StretchCluster, clusterNames []string) *stretchClusterReconciliationState {
	return &stretchClusterReconciliationState{
		cluster: lifecycle.NewStretchClusterWithPools(sc, clusterNames),
		status:  lifecycle.NewStretchClusterStatus(),
	}
}

func testStretchCluster() *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stretch",
			Namespace: "default",
		},
		Spec: redpandav1alpha2.StretchClusterSpec{
			Auth: &redpandav1alpha2.Auth{
				SASL: &redpandav1alpha2.SASL{
					Enabled: ptr.To(true),
				},
			},
		},
	}
}

func stretchClusterForCluster(sc *redpandav1alpha2.StretchCluster, clusterName string) *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sc.Name,
			Namespace: sc.Namespace,
			UID:       types.UID("uid-" + clusterName),
		},
		Spec: sc.Spec,
	}
}

func TestSyncBootstrapUser_NoExistingSecrets(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b", "cluster-c"}
	sc := testStretchCluster()
	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a")),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
		"cluster-c": newFakeClient(stretchClusterForCluster(sc, "cluster-c")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	result, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)

	// Verify password was generated and stored on state.
	require.NotEmpty(t, state.bootstrapPassword)
	require.Len(t, state.bootstrapPassword, 32)
	require.Equal(t, defaultBootstrapUsername, state.bootstrapUser)

	// Verify secret was created in all clusters with the same password.
	for _, clusterName := range clusterNames {
		var secret corev1.Secret
		secretName := bootstrapSecretName(sc)
		err := clients[clusterName].Get(ctx, types.NamespacedName{
			Namespace: sc.Namespace,
			Name:      secretName,
		}, &secret)
		require.NoError(t, err, "secret should exist in cluster %s", clusterName)
		require.Equal(t, state.bootstrapPassword, string(secret.Data[bootstrapUserPasswordKey]))
		require.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		require.NotNil(t, secret.Immutable)
		require.True(t, *secret.Immutable)
	}

	// Verify condition was set: Synced (newly generated).
	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonSynced), cond.Reason)
	require.Contains(t, cond.Message, "3 cluster(s)")
}

func TestSyncBootstrapUser_ExistingSecretInOneCluster(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	existingPassword := "pre-existing-password-1234567890"

	// cluster-a already has the secret.
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bootstrapSecretName(sc),
			Namespace: sc.Namespace,
		},
		Data: map[string][]byte{
			bootstrapUserPasswordKey: []byte(existingPassword),
		},
		Type: corev1.SecretTypeOpaque,
	}

	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"), existingSecret),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	result, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)

	// Verify the existing password was reused (not regenerated).
	require.Equal(t, existingPassword, state.bootstrapPassword)

	// Verify the same password was distributed to cluster-b.
	var secret corev1.Secret
	err = clients["cluster-b"].Get(ctx, types.NamespacedName{
		Namespace: sc.Namespace,
		Name:      bootstrapSecretName(sc),
	}, &secret)
	require.NoError(t, err)
	require.Equal(t, existingPassword, string(secret.Data[bootstrapUserPasswordKey]))

	// Verify condition was set: ExistingReused (False because the secret was not freshly generated).
	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonExistingReused), cond.Reason)
	require.Contains(t, cond.Message, "cluster-a")
}

func TestSyncBootstrapUser_AllSecretsExist(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	password := "shared-password-across-all-12345"

	clients := map[string]client.Client{}
	for _, clusterName := range clusterNames {
		clients[clusterName] = newFakeClient(stretchClusterForCluster(sc, clusterName), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bootstrapSecretName(sc),
				Namespace: sc.Namespace,
			},
			Data: map[string][]byte{
				bootstrapUserPasswordKey: []byte(password),
			},
			Type: corev1.SecretTypeOpaque,
		})
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	result, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)

	// Verify the existing password was used.
	require.Equal(t, password, state.bootstrapPassword)

	// Verify condition: ExistingReused (False because the secret was not freshly generated).
	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonExistingReused), cond.Reason)
}

func TestSyncBootstrapUser_PasswordMismatchAcrossClusters(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()

	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bootstrapSecretName(sc),
				Namespace: sc.Namespace,
			},
			Data: map[string][]byte{
				bootstrapUserPasswordKey: []byte("password-one-aaaaaaaaaaaaaaaaaaa"),
			},
			Type: corev1.SecretTypeOpaque,
		}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bootstrapSecretName(sc),
				Namespace: sc.Namespace,
			},
			Data: map[string][]byte{
				bootstrapUserPasswordKey: []byte("password-two-bbbbbbbbbbbbbbbbbbb"),
			},
			Type: corev1.SecretTypeOpaque,
		}),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{Manager: mgr}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "password mismatch")
	require.Contains(t, err.Error(), "cluster-a")
	require.Contains(t, err.Error(), "cluster-b")

	// Verify condition was set: PasswordMismatch with False status.
	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonPasswordMismatch), cond.Reason)
	require.Contains(t, cond.Message, "manual intervention")
}

func TestSyncBootstrapUser_ClusterUnreachable(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	// "cluster-b" is not in the manager's cluster map, simulating unreachable.
	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{Manager: mgr}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster-b")
}
