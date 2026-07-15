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
	// localName, when non-empty, is returned by GetLocalClusterName. It lets a
	// test model the production convention where GetClusterNames() carries the
	// empty-string local sentinel (mcmanager.LocalCluster) while the manager
	// still knows the canonical peer name. Falls back to names[0] when unset.
	localName string
}

func (m *mockManager) GetClusterNames() []string { return m.names }
func (m *mockManager) GetCluster(_ context.Context, name string) (cluster.Cluster, error) {
	cl, ok := m.clusters[name]
	if !ok {
		return nil, k8sapierrors.NewNotFound(corev1.Resource("cluster"), name)
	}
	return cl, nil
}
func (m *mockManager) GetLeader() string { return m.names[0] }
func (m *mockManager) GetLocalClusterName() string {
	if m.localName != "" {
		return m.localName
	}
	return m.names[0]
}

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

// TestSyncBootstrapUser_MismatchMessageCanonicalizesLocalCluster covers the
// K8S-900 cosmetic sub-bug: when the canonical (first-found) secret lives on the
// local cluster, GetClusterNames() reports it as the empty-string sentinel
// (mcmanager.LocalCluster). The mismatch error must surface the human-readable
// peer name (GetLocalClusterName) rather than an empty cluster name.
func TestSyncBootstrapUser_MismatchMessageCanonicalizesLocalCluster(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	// "" is mcmanager.LocalCluster — the local cluster sentinel that
	// GetClusterNames() returns in production.
	clusterNames := []string{"", "cluster-b"}
	sc := testStretchCluster()

	clients := map[string]client.Client{
		"": newFakeClient(stretchClusterForCluster(sc, "local"), &corev1.Secret{
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
	mgr.localName = "kind-rp-1"
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{Manager: mgr}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "password mismatch")
	// The canonical cluster (first found, on the local sentinel "") must be
	// reported by its human-readable name, not as an empty string.
	require.Contains(t, err.Error(), "kind-rp-1")
	require.NotContains(t, err.Error(), `cluster ""`)
}

// TestSyncBootstrapUser_ReusedMessageCanonicalizesLocalCluster is the success-path
// counterpart to the mismatch test: when the reused secret is found on the local
// cluster (the empty-string sentinel), the ExistingReused status message must name
// the canonical peer, not an empty cluster (K8S-900).
func TestSyncBootstrapUser_ReusedMessageCanonicalizesLocalCluster(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"", "cluster-b"}
	sc := testStretchCluster()
	password := "shared-password-across-all-12345"

	clients := map[string]client.Client{
		"": newFakeClient(stretchClusterForCluster(sc, "local"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretName(sc), Namespace: sc.Namespace},
			Data:       map[string][]byte{bootstrapUserPasswordKey: []byte(password)},
			Type:       corev1.SecretTypeOpaque,
		}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretName(sc), Namespace: sc.Namespace},
			Data:       map[string][]byte{bootstrapUserPasswordKey: []byte(password)},
			Type:       corev1.SecretTypeOpaque,
		}),
	}
	mgr := newMockManager(clusterNames, clients)
	mgr.localName = "kind-rp-1"
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)

	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonExistingReused), cond.Reason)
	require.Contains(t, cond.Message, "kind-rp-1")
	require.NotContains(t, cond.Message, `cluster ""`)
}

// TestSyncBootstrapUser_HonorsSecretKeyRefExisting: when the user pins the
// bootstrap password to a custom Secret/key via bootstrapUser.secretKeyRef and
// pre-creates it on one cluster, syncBootstrapUser must read that Secret (not the
// operator-managed <name>-bootstrap-user), reuse its password, and replicate it
// to the other clusters under the same custom name/key (K8S-900 follow-up).
func TestSyncBootstrapUser_HonorsSecretKeyRefExisting(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
			Key:                  "custom-key",
		},
	}
	userPassword := "user-supplied-password-abcdefghij"

	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "custom-bootstrap", Namespace: sc.Namespace},
			Data:       map[string][]byte{"custom-key": []byte(userPassword)},
			Type:       corev1.SecretTypeOpaque,
		}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)

	// The user-supplied password is reused.
	require.Equal(t, userPassword, state.bootstrapPassword)

	// The custom secret is replicated to cluster-b under the same name/key.
	var replicated corev1.Secret
	require.NoError(t, clients["cluster-b"].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: "custom-bootstrap"}, &replicated))
	require.Equal(t, userPassword, string(replicated.Data["custom-key"]))

	// The operator-managed secret name must NOT be created anywhere.
	var operatorManaged corev1.Secret
	err = clients["cluster-b"].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: bootstrapSecretName(sc)}, &operatorManaged)
	require.True(t, k8sapierrors.IsNotFound(err), "operator-managed secret must not be created when secretKeyRef is set")
}

// TestSyncBootstrapUser_HonorsSecretKeyRefGenerate: when secretKeyRef is set but
// no Secret exists yet, syncBootstrapUser generates a password and writes it to
// the referenced Secret/key (the field is documented as "where the generated
// password will be written").
func TestSyncBootstrapUser_HonorsSecretKeyRefGenerate(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
			Key:                  "custom-key",
		},
	}

	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a")),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)
	require.Len(t, state.bootstrapPassword, 32)

	for _, clusterName := range clusterNames {
		var secret corev1.Secret
		require.NoError(t, clients[clusterName].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: "custom-bootstrap"}, &secret),
			"generated secret should exist under the referenced name in %s", clusterName)
		require.Equal(t, state.bootstrapPassword, string(secret.Data["custom-key"]))
	}
}

// TestSyncBootstrapUser_SecretKeyRefExistingButMissingKey: when secretKeyRef
// points at a Secret that exists but has no data under the referenced key, the
// operator must fail loudly (terminal condition) rather than silently generate a
// divergent password that gets written only to the other clusters (K8S-900).
func TestSyncBootstrapUser_SecretKeyRefExistingButMissingKey(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
			Key:                  "custom-key",
		},
	}

	clients := map[string]client.Client{
		// The referenced Secret exists but stores the password under the wrong key.
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "custom-bootstrap", Namespace: sc.Namespace},
			Data:       map[string][]byte{"wrong-key": []byte("whatever")},
			Type:       corev1.SecretTypeOpaque,
		}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no data under key")
	require.Contains(t, err.Error(), "custom-key")

	// Must NOT have written a divergent password to cluster-b.
	var replicated corev1.Secret
	getErr := clients["cluster-b"].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: "custom-bootstrap"}, &replicated)
	require.True(t, k8sapierrors.IsNotFound(getErr), "must not create a divergent secret when the referenced key is missing")

	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonTerminalError), cond.Reason)
}

// TestSyncBootstrapUser_MigratesLegacyPasswordToSecretKeyRef: a cluster
// bootstrapped before secretKeyRef was honored holds the operator-managed
// <name>-bootstrap-user Secret. When the user later adds a secretKeyRef, the
// operator must keep using the password the cluster was actually bootstrapped
// with (the legacy one) and seed the referenced location from it — not generate
// a new password that Redpanda never saw (K8S-900).
func TestSyncBootstrapUser_MigratesLegacyPasswordToSecretKeyRef(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
			Key:                  "custom-key",
		},
	}
	legacyPassword := "legacy-bootstrapped-password-123"

	clients := map[string]client.Client{
		// cluster-a still carries the legacy operator-managed secret; the
		// referenced custom secret does not exist yet.
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretName(sc), Namespace: sc.Namespace},
			Data:       map[string][]byte{bootstrapUserPasswordKey: []byte(legacyPassword)},
			Type:       corev1.SecretTypeOpaque,
		}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)

	// The legacy (bootstrapped) password is retained, not regenerated.
	require.Equal(t, legacyPassword, state.bootstrapPassword)

	// The referenced location is seeded with the legacy password on every cluster.
	for _, clusterName := range clusterNames {
		var ref corev1.Secret
		require.NoError(t, clients[clusterName].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: "custom-bootstrap"}, &ref),
			"referenced secret should be seeded in %s", clusterName)
		require.Equal(t, legacyPassword, string(ref.Data["custom-key"]))
	}
}

// TestSyncBootstrapUser_MigrationConflictRefusesDivergence: if the referenced
// secret holds a *different* password than the one the cluster was bootstrapped
// with, the operator refuses (terminal condition) rather than write divergent
// copies to the other clusters.
func TestSyncBootstrapUser_MigrationConflictRefusesDivergence(t *testing.T) {
	ctx := ctrllog.IntoContext(context.Background(), logr.Discard())

	clusterNames := []string{"cluster-a", "cluster-b"}
	sc := testStretchCluster()
	sc.Spec.Auth.SASL.BootstrapUser = &redpandav1alpha2.BootstrapUser{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "custom-bootstrap"},
			Key:                  "custom-key",
		},
	}

	clients := map[string]client.Client{
		"cluster-a": newFakeClient(stretchClusterForCluster(sc, "cluster-a"),
			// legacy operator-managed (bootstrapped) password
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecretName(sc), Namespace: sc.Namespace},
				Data:       map[string][]byte{bootstrapUserPasswordKey: []byte("legacy-password-aaaaaaaaaaaaaaa")},
				Type:       corev1.SecretTypeOpaque,
			},
			// referenced location holds a DIFFERENT password
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "custom-bootstrap", Namespace: sc.Namespace},
				Data:       map[string][]byte{"custom-key": []byte("user-password-bbbbbbbbbbbbbbbb")},
				Type:       corev1.SecretTypeOpaque,
			}),
		"cluster-b": newFakeClient(stretchClusterForCluster(sc, "cluster-b")),
	}
	mgr := newMockManager(clusterNames, clients)
	state := newTestState(sc, clusterNames)

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "different password")

	// Must not have written a divergent copy to cluster-b.
	var ref corev1.Secret
	getErr := clients["cluster-b"].Get(ctx, types.NamespacedName{Namespace: sc.Namespace, Name: "custom-bootstrap"}, &ref)
	require.True(t, k8sapierrors.IsNotFound(getErr))

	state.status.StretchClusterStatus.UpdateConditions(sc)
	cond := apimeta.FindStatusCondition(sc.Status.Conditions, statuses.StretchClusterBootstrapUserSynced)
	require.NotNil(t, cond)
	require.Equal(t, string(statuses.StretchClusterBootstrapUserSyncedReasonTerminalError), cond.Reason)
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

	r := &MulticlusterReconciler{
		Manager:         mgr,
		LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(lifecycle.Image{}, lifecycle.Image{}, lifecycle.CloudSecretsFlags{})),
	}
	_, err := r.syncBootstrapUser(ctx, state, nil)
	require.NoError(t, err)

	// cluster-a is reachable: secret should be created.
	var secret corev1.Secret
	require.NoError(t, clients["cluster-a"].Get(ctx, types.NamespacedName{
		Namespace: sc.Namespace,
		Name:      bootstrapSecretName(sc),
	}, &secret))
	require.NotEmpty(t, secret.Data[bootstrapUserPasswordKey])
}
