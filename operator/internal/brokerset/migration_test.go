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
	"fmt"
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
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// TestEnsureMigrationHandsOverWithDeleteRetentionPolicy runs the state
// machine against a StatefulSet whose PVC retention policy is Delete/Delete,
// as --auto-delete-pvcs renders it: the handover orphan-deletes it as is, the
// backup carries the policy, and rollback restores it.
func TestEnsureMigrationHandsOverWithDeleteRetentionPolicy(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))

	const checksumKey = "redpanda.com/config-checksum"
	deleteAll := &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
		WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
		WhenScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
	}

	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", UID: "owner-uid"}}
	live := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", Generation: 1},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    ptr.To(int32(2)),
			ServiceName: "rp",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "redpanda"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": "redpanda"},
					Annotations: map[string]string{checksumKey: "abc"},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda:latest"}}},
			},
			VolumeClaimTemplates:                 []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "datadir"}}},
			PersistentVolumeClaimRetentionPolicy: deleteAll,
		},
		// Converged, so the migration preconditions pass.
		Status: appsv1.StatefulSetStatus{ObservedGeneration: 1, Replicas: 2, ReadyReplicas: 2},
	}
	desired := live.DeepCopy()

	objs := []k8sclient.Object{owner, live}
	for i := range 2 {
		objs = append(objs, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("rp-%d", i),
			Namespace:   "test",
			Labels:      map[string]string{"app": "redpanda"},
			Annotations: map[string]string{checksumKey: "abc"},
		}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	s := &BrokerSet{
		Client:            c,
		Scheme:            scheme,
		Owner:             owner,
		ClusterRef:        redpandav1alpha2.ClusterRef{Name: "rp"},
		PoolName:          "default",
		BrokerLabels:      map[string]string{"app": "redpanda"},
		PoolSelector:      k8slabels.Everything(),
		ClusterSelector:   k8slabels.Everything(),
		PodSelector:       k8slabels.Everything(),
		ConfigChecksumKey: checksumKey,
		Hooks:             quiescentHooks{},
		Logger:            logr.Discard(),
	}

	// Shadow Broker CRs from an earlier pass, so the state machine reaches
	// the handover step.
	shadows, err := s.RenderBrokers(desired, 2, true)
	require.NoError(t, err)
	for i := range shadows {
		shadows[i].Name = fmt.Sprintf("rp-%d", i)
		require.NoError(t, c.Create(ctx, &shadows[i]))
	}

	require.NoError(t, s.ensureMigration(ctx, logr.Discard(), live, desired))
	var gone appsv1.StatefulSet
	require.True(t, apierrors.IsNotFound(c.Get(ctx, k8sclient.ObjectKeyFromObject(live), &gone)),
		"a Delete retention policy must not hold up the handover")

	var cm corev1.ConfigMap
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: migrationBackupName(owner.Name), Namespace: "test"}, &cm))
	var backup appsv1.StatefulSet
	require.NoError(t, json.Unmarshal([]byte(cm.Data["default.json"]), &backup))
	require.Equal(t, deleteAll, backup.Spec.PersistentVolumeClaimRetentionPolicy)

	// Rollback restores the StatefulSet with the same policy, then waits
	// for it to adopt the pods.
	_, err = Rollback(ctx, RollbackConfig{
		Client:          c,
		Scheme:          scheme,
		Owner:           owner,
		ClusterSelector: k8slabels.Everything(),
		Logger:          logr.Discard(),
	})
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)
	var restored appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, k8sclient.ObjectKeyFromObject(live), &restored))
	require.Equal(t, deleteAll, restored.Spec.PersistentVolumeClaimRetentionPolicy)
}

// quiescentHooks is a healthy, idle owner.
type quiescentHooks struct{}

func (quiescentHooks) IsClusterHealthy(context.Context) error        { return nil }
func (quiescentHooks) OnQuiesced(context.Context) error              { return nil }
func (quiescentHooks) MigrationBlockedReason(context.Context) string { return "" }
