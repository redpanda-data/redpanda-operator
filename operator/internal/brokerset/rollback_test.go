// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package brokerset

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

type recordingReporter struct {
	reason  string
	reports []string
}

func (r *recordingReporter) Report(_ context.Context, _ corev1.ConditionStatus, reason, _ string) {
	r.reports = append(r.reports, reason)
	r.reason = reason
}

func (r *recordingReporter) shouldReport(terminalReason string) bool {
	return r.reason != "" && r.reason != terminalReason
}

func (r *recordingReporter) ShouldReportComplete(context.Context) bool {
	return r.shouldReport(MigrationReasonComplete)
}

func (r *recordingReporter) ShouldReportRolledBack(context.Context) bool {
	return r.shouldReport(MigrationReasonRolledBack)
}

func TestRollbackTouchesOnlyOwnedBrokers(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"}}
	foreignOwner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "other-cluster", Namespace: "test", UID: "foreign-uid"}}

	foreign := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-cluster-broker-0",
			Namespace: "test",
			// Labels that match the rollback's selector (Everything() here;
			// in production: copyable name-based owner labels).
			Labels: map[string]string{"app": "redpanda"},
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef:   redpandav1alpha2.ClusterRef{Name: "other-cluster"},
			NetworkIndex: ptr.To(int32(0)),
		},
	}
	require.NoError(t, controllerutil.SetControllerReference(foreignOwner, foreign, scheme))

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, foreignOwner, foreign).Build()

	acted, err := Rollback(ctx, RollbackConfig{
		Client:          c,
		Scheme:          scheme,
		Owner:           owner,
		ClusterSelector: k8slabels.Everything(),
		Logger:          logr.Discard(),
	})
	require.NoError(t, err)
	require.False(t, acted, "a foreign Broker must not count as this owner's rollback work")

	var still redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: foreign.Name, Namespace: "test"}, &still),
		"rollback deleted a Broker CR controlled by a different owner")
	require.Zero(t, still.DeletionTimestamp, "rollback must not delete a Broker CR it does not own")
}

func TestRollbackRederivesLostTerminalReport(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	// Steady state: no Broker CRs, no backup ConfigMap.
	rollback := func(t *testing.T, rep *recordingReporter) {
		acted, err := Rollback(context.Background(), RollbackConfig{
			Client:          fake.NewClientBuilder().WithScheme(scheme).Build(),
			Scheme:          scheme,
			Owner:           &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"}},
			ClusterSelector: k8slabels.Everything(),
			Reporter:        rep,
			Logger:          logr.Discard(),
		})
		require.NoError(t, err)
		require.False(t, acted)
	}

	t.Run("lost RolledBack is re-reported", func(t *testing.T) {
		rep := &recordingReporter{reason: MigrationReasonInProgress}
		rollback(t, rep)
		require.Equal(t, []string{MigrationReasonRolledBack}, rep.reports,
			"a rollback whose terminal report never persisted must be promoted to RolledBack in steady state")
	})

	t.Run("never-migrated cluster reports nothing", func(t *testing.T) {
		rep := &recordingReporter{}
		rollback(t, rep)
		require.Empty(t, rep.reports, "a cluster with no migration condition must not get one")
	})

	t.Run("already rolled back reports nothing", func(t *testing.T) {
		rep := &recordingReporter{reason: MigrationReasonRolledBack}
		rollback(t, rep)
		require.Empty(t, rep.reports, "a persisted RolledBack must not be re-written every pass")
	})
}

func TestFinalizeRollbackIgnoresForeignPods(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"}}

	backup, err := json.Marshal(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
		Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
	})
	require.NoError(t, err)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: migrationBackupName(owner.Name), Namespace: "test"},
		Data:       map[string]string{"rp.json": string(backup)},
	}

	// This rollback's pod: StatefulSet-owned and already carrying a revision
	// label, so the flow can complete in one pass.
	adopted := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp-0",
			Namespace: "test",
			Labels:    map[string]string{appsv1.StatefulSetRevisionLabel: "rp-abc"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "StatefulSet", Name: "rp", UID: "sts-uid", Controller: ptr.To(true),
			}},
		},
	}

	// A label-matching pod from ANOTHER cluster, still owned by its Broker CR.
	foreignBrokerRef := metav1.OwnerReference{
		APIVersion: redpandav1alpha2.GroupVersion.String(),
		Kind:       redpandav1alpha2.BrokerKind,
		Name:       "other-cluster-broker-0",
		UID:        "foreign-broker-uid",
		Controller: ptr.To(true),
	}
	foreignPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-cluster-0", Namespace: "test", OwnerReferences: []metav1.OwnerReference{foreignBrokerRef}},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
			Name:         "datadir",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "datadir-other-cluster-0"}},
		}}},
	}
	foreignPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "datadir-other-cluster-0", Namespace: "test", OwnerReferences: []metav1.OwnerReference{foreignBrokerRef}},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, cm, adopted, foreignPod, foreignPVC).Build()

	_, err = Rollback(ctx, RollbackConfig{
		Client:          c,
		Scheme:          scheme,
		Owner:           owner,
		ClusterSelector: k8slabels.Everything(),
		Logger:          logr.Discard(),
	})
	require.NoError(t, err, "a foreign pod must not wedge the rollback at the adoption wait")

	var gone corev1.ConfigMap
	require.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: "test"}, &gone)),
		"rollback must complete (backup deleted) despite the foreign pod")

	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: foreignPod.Name, Namespace: "test"}, &pod))
	require.Equal(t, []metav1.OwnerReference{foreignBrokerRef}, pod.OwnerReferences,
		"rollback stripped the Broker ownerRef from a pod it does not own")
	require.Empty(t, pod.Labels[appsv1.StatefulSetRevisionLabel],
		"rollback stamped a revision label onto a pod it does not own")

	var pvc corev1.PersistentVolumeClaim
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: foreignPVC.Name, Namespace: "test"}, &pvc))
	require.Equal(t, []metav1.OwnerReference{foreignBrokerRef}, pvc.OwnerReferences,
		"rollback stripped the Broker ownerRef from a claim it does not own")
}
