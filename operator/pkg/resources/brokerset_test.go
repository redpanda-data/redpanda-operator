// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//nolint:testpackage // the tests use private methods
package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

const (
	testCurrentChecksum = "current-checksum"
	testOldChecksum     = "old-checksum"
)

func brokerSetTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, vectorizedv1alpha1.Install(scheme))
	require.NoError(t, redpandav1alpha2.Install(scheme))
	return scheme
}

func brokerSetTestCluster() *vectorizedv1alpha1.Cluster {
	return &vectorizedv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp",
			Namespace: "test",
			UID:       types.UID("cluster-uid"),
		},
	}
}

type testBroker struct {
	index        int32
	podChecksum  string // "" = no pod
	podReady     bool
	grant        string
	decommission bool
	phase        redpandav1alpha2.BrokerPhase
	brokerID     *int32
}

// buildBrokerSet constructs a BrokerSetResource backed by a fake client
// pre-populated with the cluster, Broker CRs and pods described by brokers.
func buildBrokerSet(t *testing.T, healthy bool, brokers []testBroker, interceptors interceptor.Funcs) (*BrokerSetResource, k8sclient.Client) {
	scheme := brokerSetTestScheme(t)
	cluster := brokerSetTestCluster()

	objs := []k8sclient.Object{cluster}
	clusterLabels := labels.ForCluster(cluster)
	for _, tb := range brokers {
		broker := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("rp-broker-%d", tb.index),
				Namespace: cluster.Namespace,
				Labels:    clusterLabels,
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef: redpandav1alpha2.ClusterRef{
					Group: ptr.To("redpanda.vectorized.io"),
					Kind:  ptr.To("Cluster"),
					Name:  cluster.Name,
				},
				NetworkIndex: ptr.To(tb.index),
				Decommission: tb.decommission,
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Annotations: map[string]string{
						redpandav1alpha2.BrokerConfigChecksumAnnotation: testCurrentChecksum,
					},
				},
			},
			Status: redpandav1alpha2.BrokerStatus{
				Phase:    tb.phase,
				BrokerID: tb.brokerID,
			},
		}
		if tb.brokerID != nil {
			// A registered fixture broker also carries the verified
			// BrokerRegistered condition the Broker controller would set.
			apimeta.SetStatusCondition(&broker.Status.Conditions, metav1.Condition{
				Type: "BrokerRegistered", Status: metav1.ConditionTrue,
				Reason: "Registered", Message: fmt.Sprintf("Broker ID %d", *tb.brokerID),
			})
		}
		if tb.grant != "" {
			broker.Annotations = map[string]string{feature.RollGrant.Key: tb.grant}
		}
		require.NoError(t, controllerutil.SetControllerReference(cluster, broker, scheme))
		objs = append(objs, broker)

		if tb.podChecksum != "" {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      broker.PodName(),
					Namespace: cluster.Namespace,
					Annotations: map[string]string{
						redpandav1alpha2.BrokerConfigChecksumAnnotation: tb.podChecksum,
					},
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
			}
			if tb.podReady {
				pod.Status.Phase = corev1.PodRunning
				pod.Status.Conditions = []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				}
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "redpanda", Ready: true}}
			}
			objs = append(objs, pod)
		}
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(runtimeObjects(objs)...).
		WithStatusSubresource(&vectorizedv1alpha1.Cluster{}).
		WithInterceptorFuncs(interceptors).
		Build()

	sts := &StatefulSetResource{
		Client:       c,
		pandaCluster: cluster,
		logger:       ctrl.Log.WithName("test"),
		adminAPIClientFactory: func(ctx context.Context, k8sClient k8sclient.Reader, redpandaCluster *vectorizedv1alpha1.Cluster, fqdn string, adminTLSProvider resourcetypes.AdminTLSConfigProvider, dialer redpanda.DialContextFunc, timeout time.Duration, pods ...string) (adminutils.AdminAPIClient, error) {
			adminAPI := &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")}
			adminAPI.SetClusterHealth(healthy)
			return adminAPI, nil
		},
	}

	return &BrokerSetResource{
		Client:       c,
		scheme:       scheme,
		pandaCluster: cluster,
		stsResource:  sts,
		logger:       ctrl.Log.WithName("test"),
	}, c
}

func runtimeObjects(objs []k8sclient.Object) []k8sclient.Object {
	return objs
}

func listGrantedBrokers(t *testing.T, c k8sclient.Client) []string {
	var list redpandav1alpha2.BrokerList
	require.NoError(t, c.List(context.Background(), &list))
	var granted []string
	for _, b := range list.Items {
		if b.Annotations[feature.RollGrant.Key] != "" {
			granted = append(granted, b.Name)
		}
	}
	return granted
}

func TestEnsureRollGrantsGrantsExactlyOne(t *testing.T) {
	// All three brokers are outdated; exactly one (lowest index) is granted.
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
		{index: 1, podChecksum: testOldChecksum, podReady: true},
		{index: 2, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.ensureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	granted := listGrantedBrokers(t, c)
	require.Equal(t, []string{"rp-broker-0"}, granted)

	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b))
	checksum, deadline, ok := feature.ParseRollGrant(b.Annotations[feature.RollGrant.Key])
	require.True(t, ok)
	assert.Equal(t, testCurrentChecksum, checksum)
	assert.True(t, deadline.After(time.Now()))
}

func TestEnsureRollGrantsNoSecondGrantWhileActive(t *testing.T) {
	// Broker 0 holds an unexpired grant (mid-roll, pod deleted); broker 1 is
	// outdated but must NOT be granted.
	activeGrant := feature.FormatRollGrant(testCurrentChecksum, time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: activeGrant}, // no pod: mid-rotation
		{index: 1, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.ensureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	granted := listGrantedBrokers(t, c)
	assert.Equal(t, []string{"rp-broker-0"}, granted)
}

func TestEnsureRollGrantsRevokesOnCompletion(t *testing.T) {
	// Broker 0 finished its roll: pod matches the desired checksum, is ready,
	// and the broker is registered. The grant must be revoked; with nothing
	// left to roll the pass is a no-op.
	activeGrant := feature.FormatRollGrant(testCurrentChecksum, time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: activeGrant, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
		{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(1))},
	}, interceptor.Funcs{})

	require.NoError(t, r.ensureRollGrants(context.Background(), ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsRevokesStaleChecksum(t *testing.T) {
	// A grant minted for a previous config generation is stale and revoked,
	// even though its deadline has not passed.
	staleGrant := feature.FormatRollGrant(testOldChecksum, time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: staleGrant, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
	}, interceptor.Funcs{})

	require.NoError(t, r.ensureRollGrants(context.Background(), ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsRegrantsExpired(t *testing.T) {
	// Broker 1 holds an EXPIRED grant on an unfinished roll: it is re-granted
	// with priority over broker 0, which is also outdated.
	expiredGrant := feature.FormatRollGrant(testCurrentChecksum, time.Now().Add(-time.Minute))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
		{index: 1, grant: expiredGrant, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.ensureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	granted := listGrantedBrokers(t, c)
	require.Equal(t, []string{"rp-broker-1"}, granted)

	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-1", Namespace: "test"}, &b))
	_, deadline, ok := feature.ParseRollGrant(b.Annotations[feature.RollGrant.Key])
	require.True(t, ok)
	assert.True(t, deadline.After(time.Now()), "expired grant must be re-stamped with a fresh deadline")
}

func TestEnsureRollGrantsRestartMarker(t *testing.T) {
	// A restart-requiring central-config change stamps the config version
	// into the Broker's desired pod template: the live pod (created before
	// the marker) drifts and the broker becomes a roll candidate even though
	// its checksum is current.
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
		{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(1))},
	}, interceptor.Funcs{})

	require.NoError(t, MarkBrokersForRestart(ctx, c, r.pandaCluster, 7))

	// The marker lands on Broker SPECS only — pods are never touched.
	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-0", Namespace: "test"}, &pod))
	assert.NotContains(t, pod.Annotations, redpandav1alpha2.BrokerClusterConfigVersionAnnotation)

	err := r.ensureRollGrants(ctx, ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)
	assert.Equal(t, []string{"rp-broker-0"}, listGrantedBrokers(t, c))
}

func TestUpdateBrokerPreservesRestartMarker(t *testing.T) {
	// The restart marker is stamped by MarkBrokersForRestart, not the
	// renderer: a subsequent PodTemplate sync must not wipe it before the
	// restart has rolled through.
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})
	require.NoError(t, MarkBrokersForRestart(ctx, c, r.pandaCluster, 7))

	var existing redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &existing))
	desired := existing.DeepCopy()
	delete(desired.Spec.PodTemplate.Annotations, redpandav1alpha2.BrokerClusterConfigVersionAnnotation)
	desired.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = "next-checksum"

	require.NoError(t, r.updateBroker(ctx, ctrl.Log, &existing, desired))

	var updated redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &updated))
	assert.Equal(t, "7", updated.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation])
	assert.Equal(t, "next-checksum", updated.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation])
}

func TestEnsureRollGrantsClearsRestarting(t *testing.T) {
	// Once no pod needs a roll and no grant is active, a pending Restarting
	// status is cleared (broker-mode counterpart of the STS rolling-update
	// path).
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
	}, interceptor.Funcs{})

	r.pandaCluster.Status.NodePools = map[string]vectorizedv1alpha1.NodePoolStatus{
		r.nodePool.Name: {Restarting: true},
	}
	r.pandaCluster.Status.SetRestarting(true)
	require.NoError(t, c.Status().Update(context.Background(), r.pandaCluster))

	require.NoError(t, r.ensureRollGrants(context.Background(), ctrl.Log))

	assert.False(t, r.pandaCluster.Status.IsRestarting())
	var persisted vectorizedv1alpha1.Cluster
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: r.pandaCluster.Name, Namespace: r.pandaCluster.Namespace}, &persisted))
	assert.False(t, persisted.Status.IsRestarting())
}

func TestEnsureRollGrantsUnhealthyCluster(t *testing.T) {
	// An outdated broker is NOT granted while the cluster is unhealthy.
	r, c := buildBrokerSet(t, false, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.ensureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsHoldsDuringDecommission(t *testing.T) {
	// No grants are issued while a decommission is in flight.
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
		{index: 2, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum},
	}, interceptor.Funcs{})

	require.NoError(t, r.ensureRollGrants(context.Background(), ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsQuiescentClusterNoWrites(t *testing.T) {
	// Nothing outdated, no grants: the pass must not write anything at all.
	// The interceptors fail the test on any mutating call.
	failWrites := interceptor.Funcs{
		Update: func(ctx context.Context, client k8sclient.WithWatch, obj k8sclient.Object, opts ...k8sclient.UpdateOption) error {
			t.Fatalf("unexpected Update of %T %s", obj, obj.GetName())
			return nil
		},
		Patch: func(ctx context.Context, client k8sclient.WithWatch, obj k8sclient.Object, patch k8sclient.Patch, opts ...k8sclient.PatchOption) error {
			t.Fatalf("unexpected Patch of %T %s", obj, obj.GetName())
			return nil
		},
	}
	r, _ := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
		{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(1))},
	}, failWrites)

	require.NoError(t, r.ensureRollGrants(context.Background(), ctrl.Log))
}

func TestRollbackRestoresStatefulSetFromBackup(t *testing.T) {
	// Rollback recreates the STS from the migration backup ConfigMap (RFC
	// Q1) rather than re-rendering, then deletes the backup.
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})

	backupSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test", Labels: map[string]string{"from": "backup"}},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{ConfigMapHashAnnotationKey: testCurrentChecksum},
				},
			},
		},
	}
	data, err := json.Marshal(backupSTS)
	require.NoError(t, err)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "rp-migration-backup", Namespace: "test"},
		Data:       map[string]string{"default.json": string(data)},
	}
	require.NoError(t, c.Create(ctx, cm))

	require.NoError(t, RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log))

	var restored appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &restored))
	assert.Equal(t, "backup", restored.Labels["from"], "STS must come from the backup, not a re-render")
	assert.Equal(t, ptr.To(int32(1)), restored.Spec.Replicas)
	owner := metav1.GetControllerOf(&restored)
	require.NotNil(t, owner)
	assert.Equal(t, r.pandaCluster.Name, owner.Name)

	var gone corev1.ConfigMap
	err = c.Get(ctx, types.NamespacedName{Name: "rp-migration-backup", Namespace: "test"}, &gone)
	assert.True(t, apierrors.IsNotFound(err), "backup ConfigMap should be deleted after restore")

	var persisted vectorizedv1alpha1.Cluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &persisted))
	cond := persisted.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
	require.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
	assert.Equal(t, vectorizedv1alpha1.BrokerMigrationReasonRolledBack, cond.Reason)
}

func TestRollbackWithoutBackupFallsBack(t *testing.T) {
	// No backup ConfigMap: rollback still cleans up Broker CRs and simply
	// leaves STS creation to the cluster controller's render path.
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})

	require.NoError(t, RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log))

	var brokers redpandav1alpha2.BrokerList
	require.NoError(t, c.List(ctx, &brokers))
	assert.Empty(t, brokers.Items, "Broker CRs should be cleaned up")
	var sts appsv1.StatefulSet
	err := c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &sts)
	assert.True(t, apierrors.IsNotFound(err), "no STS should be restored without a backup")
}

func TestUpdateBrokerSkipsNoopWrites(t *testing.T) {
	// updateBroker must not issue an API write when the PodTemplate is
	// unchanged — an unconditional write here previously caused a
	// self-sustaining reconcile hot loop via the cluster controller's
	// Owns(&Broker{}) watch.
	failWrites := interceptor.Funcs{
		Update: func(ctx context.Context, client k8sclient.WithWatch, obj k8sclient.Object, opts ...k8sclient.UpdateOption) error {
			t.Fatalf("unexpected Update of %T %s", obj, obj.GetName())
			return nil
		},
	}
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, failWrites)

	var existing redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &existing))
	desired := existing.DeepCopy()

	require.NoError(t, r.updateBroker(context.Background(), ctrl.Log, &existing, desired))
}

// migrationFixture builds a quiescent live STS + matching desired STS + ready
// pods for verifyMigrationPreconditions tests. Tests mutate the returned
// objects to violate individual preconditions.
type migrationFixture struct {
	live    *appsv1.StatefulSet
	desired *appsv1.StatefulSet
	pods    []*corev1.Pod
}

func quiescentMigrationFixture(cluster *vectorizedv1alpha1.Cluster, replicas int32) *migrationFixture {
	podLabels := labels.ForCluster(cluster).WithNodePool("default")
	live := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       cluster.Name,
			Namespace:  cluster.Namespace,
			Generation: 3,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(replicas),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{ConfigMapHashAnnotationKey: testCurrentChecksum},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 3,
			Replicas:           replicas,
			ReadyReplicas:      replicas,
			UpdatedReplicas:    replicas,
			CurrentRevision:    "rev-1",
			UpdateRevision:     "rev-1",
		},
	}
	desired := live.DeepCopy()

	var pods []*corev1.Pod
	for i := int32(0); i < replicas; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-%d", cluster.Name, i),
				Namespace:   cluster.Namespace,
				Labels:      podLabels,
				Annotations: map[string]string{ConfigMapHashAnnotationKey: testCurrentChecksum},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "redpanda", Image: "redpanda"}}},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "redpanda", Ready: true}},
			},
		}
		pods = append(pods, pod)
	}
	return &migrationFixture{live: live, desired: desired, pods: pods}
}

func buildMigrationBrokerSet(t *testing.T, healthy bool, cluster *vectorizedv1alpha1.Cluster, fix *migrationFixture) *BrokerSetResource {
	scheme := brokerSetTestScheme(t)
	objs := []k8sclient.Object{cluster, fix.live}
	for _, p := range fix.pods {
		objs = append(objs, p)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&vectorizedv1alpha1.Cluster{}).
		Build()

	sts := &StatefulSetResource{
		Client:       c,
		pandaCluster: cluster,
		nodePool:     vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{Name: "default"}},
		logger:       ctrl.Log.WithName("test"),
		adminAPIClientFactory: func(ctx context.Context, k8sClient k8sclient.Reader, redpandaCluster *vectorizedv1alpha1.Cluster, fqdn string, adminTLSProvider resourcetypes.AdminTLSConfigProvider, dialer redpanda.DialContextFunc, timeout time.Duration, pods ...string) (adminutils.AdminAPIClient, error) {
			adminAPI := &adminutils.MockAdminAPI{Log: ctrl.Log.WithName("mockAdminAPI")}
			adminAPI.SetClusterHealth(healthy)
			return adminAPI, nil
		},
	}
	return &BrokerSetResource{
		Client:       c,
		scheme:       scheme,
		pandaCluster: cluster,
		stsResource:  sts,
		nodePool:     sts.nodePool,
		logger:       ctrl.Log.WithName("test"),
	}
}

func requireMigrationBlocked(t *testing.T, err error, reason string) {
	t.Helper()
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)
	assert.Contains(t, requeue.Msg, reason)
}

func TestVerifyMigrationPreconditions(t *testing.T) {
	ctx := context.Background()

	t.Run("quiescent cluster passes", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		require.NoError(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired))
	})

	t.Run("blocked while restarting", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		cluster.Status.SetRestarting(true)
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "restarting")
	})

	t.Run("blocked while decommissioning", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		cluster.Status.DecommissioningNode = ptr.To(int32(2))
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "decommission")
	})

	t.Run("blocked while STS rollout incomplete", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.live.Status.ReadyReplicas = 2
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "rollout has not completed")

		// The blockage is surfaced on the Cluster's BrokerMigration condition.
		var persisted vectorizedv1alpha1.Cluster
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &persisted))
		cond := persisted.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
		require.NotNil(t, cond)
		assert.Equal(t, corev1.ConditionFalse, cond.Status)
		assert.Equal(t, vectorizedv1alpha1.BrokerMigrationReasonBlocked, cond.Reason)
	})

	// Regression: after a rollback the StatefulSet adopts pods created by the
	// Broker controller. Those pods have no controller-revision-hash label, so
	// UpdatedReplicas stays 0 and (under OnDelete) never converges. Migration
	// must not depend on revision bookkeeping or re-migration wedges forever.
	t.Run("passes with adopted pods lacking revision labels", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.live.Status.UpdatedReplicas = 0
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		require.NoError(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired))
	})

	t.Run("blocked while config change pending on STS", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.desired.Spec.Template.Annotations[ConfigMapHashAnnotationKey] = "next-checksum"
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "config change pending")
	})

	t.Run("blocked while a pod runs stale config", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.pods[1].Annotations[ConfigMapHashAnnotationKey] = testOldChecksum
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "not running the desired configuration")
	})

	t.Run("blocked while a pod is not ready", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.pods[2].Status.Conditions = nil
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "not ready")
	})

	t.Run("blocked while cluster unhealthy", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, false, cluster, fix)
		var requeue *RequeueAfterError
		require.ErrorAs(t, r.verifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), &requeue)
	})
}

func TestVerifyRollbackPreconditions(t *testing.T) {
	ctx := context.Background()

	build := func(t *testing.T, brokers []testBroker) ([]redpandav1alpha2.Broker, k8sclient.Client) {
		_, c := buildBrokerSet(t, true, brokers, interceptor.Funcs{})
		var list redpandav1alpha2.BrokerList
		require.NoError(t, c.List(ctx, &list))
		return list.Items, c
	}

	t.Run("all pods present passes", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true},
		})
		require.NoError(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers))
	})

	t.Run("blocked by in-flight decommission", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum},
		})
		requireMigrationBlocked(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers), "decommissioning")
	})

	t.Run("fully decommissioned broker does not block", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		})
		require.NoError(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers))
	})

	t.Run("blocked by active roll-grant", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true, grant: feature.FormatRollGrant(testCurrentChecksum, time.Now().Add(feature.RollGrantTTL))},
		})
		requireMigrationBlocked(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers), "roll-grant")
	})

	t.Run("expired roll-grant does not block", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true, grant: feature.FormatRollGrant(testCurrentChecksum, time.Now().Add(-time.Minute))},
		})
		require.NoError(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers))
	})

	t.Run("blocked by missing pod", func(t *testing.T) {
		brokers, c := build(t, []testBroker{
			{index: 0}, // no pod: rotation in flight
		})
		requireMigrationBlocked(t, verifyRollbackPreconditions(ctx, c, ctrl.Log, brokers), "missing")
	})
}

func TestUpdateBrokerWritesOnChange(t *testing.T) {
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})

	var existing redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &existing))
	desired := existing.DeepCopy()
	desired.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = "next-checksum"

	require.NoError(t, r.updateBroker(context.Background(), ctrl.Log, &existing, desired))

	var updated redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &updated))
	assert.Equal(t, "next-checksum", updated.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation])
	// The grant is NOT stamped by updateBroker — that is ensureRollGrants'
	// job, one broker at a time.
	assert.Empty(t, updated.Annotations[feature.RollGrant.Key])
}

func TestDecommissionIntentIsNeverUnset(t *testing.T) {
	// Decommission intent — set by scale-down or manually — must survive
	// reconciliation even when the index is desired again (recommission was
	// dropped from RFC Q2). The index heals by replacement instead.
	ctx := context.Background()

	t.Run("in-flight decommission on a desired index is left alone", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 2, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum, podReady: true},
		}, interceptor.Funcs{})

		var existing redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &existing))
		desired := existing.DeepCopy()
		desired.Spec.Decommission = false
		// A pending pod-template change must not be synced onto (nor unset
		// the intent of) a decommissioning broker.
		desired.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = "next-checksum"

		require.NoError(t, r.ensureDesiredBroker(ctx, ctrl.Log, &existing, desired))

		var updated redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &updated))
		assert.True(t, updated.Spec.Decommission, "decommission intent must never be unset by the operator")
		assert.NotEqual(t, "next-checksum", updated.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation])
	})

	t.Run("terminal decommissioned broker at a desired index is deleted for replacement", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 2, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		}, interceptor.Funcs{})

		var existing redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &existing))
		desired := existing.DeepCopy()
		desired.Spec.Decommission = false

		require.NoError(t, r.ensureDesiredBroker(ctx, ctrl.Log, &existing, desired))

		var deleted redpandav1alpha2.Broker
		err := c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &deleted)
		assert.True(t, apierrors.IsNotFound(err), "terminal Broker at a desired index must be deleted so a fresh one replaces it, got err=%v", err)
	})

	t.Run("updateBroker does not touch the decommission field", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 2, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum, podReady: true},
		}, interceptor.Funcs{})

		var existing redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &existing))
		desired := existing.DeepCopy()
		desired.Spec.Decommission = false
		desired.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = "next-checksum"

		require.NoError(t, r.updateBroker(ctx, ctrl.Log, &existing, desired))

		var updated redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-2", Namespace: "test"}, &updated))
		assert.True(t, updated.Spec.Decommission)
	})
}

func TestUpdateBrokerSyncsDeletionPolicy(t *testing.T) {
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})

	var existing redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &existing))
	desired := existing.DeepCopy()
	desired.Annotations = map[string]string{feature.BrokerDeletionPolicy.Key: "orphan"}

	require.NoError(t, r.updateBroker(context.Background(), ctrl.Log, &existing, desired))

	var updated redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &updated))
	assert.Equal(t, "orphan", updated.Annotations[feature.BrokerDeletionPolicy.Key])
}

func TestBrokersFromStatefulSetPodNameConsistency(t *testing.T) {
	// For every pool, the rendered Broker's PodName() must resolve to the
	// pod the renderer targets (Hostname): <cluster>-<ordinal> for the
	// default pool, <cluster>-<pool>-<ordinal> for named pools.
	cluster := brokerSetTestCluster()
	scheme := brokerSetTestScheme(t)

	cases := []struct {
		pool    string
		stsName string
	}{
		{pool: "default", stsName: "rp"},
		{pool: "high-mem", stsName: "rp-high-mem"},
	}
	for _, tc := range cases {
		t.Run(tc.pool, func(t *testing.T) {
			sts := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: tc.stsName, Namespace: cluster.Namespace},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(2)),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{ConfigMapHashAnnotationKey: testCurrentChecksum},
						},
					},
				},
			}
			nodePool := vectorizedv1alpha1.NodePoolSpecWithDeleted{
				NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{Name: tc.pool, Replicas: ptr.To(int32(2))},
			}

			brokers, err := brokersFromStatefulSet(cluster, sts, nodePool, scheme, false)
			require.NoError(t, err)
			require.Len(t, brokers, 2)
			for i := range brokers {
				b := &brokers[i]
				assert.Equal(t, fmt.Sprintf("%s-%d", tc.stsName, *b.Spec.NetworkIndex), b.PodName())
				assert.Equal(t, b.Spec.PodTemplate.Spec.Hostname, b.PodName(),
					"PodName must match the hostname the renderer stamps")
			}
		})
	}
}

func TestBrokersFromStatefulSetNormalizesDefaults(t *testing.T) {
	// The Broker CRD structurally defaults container port protocol to TCP on
	// write. The rendered template must already carry the default, or the
	// in-memory desired never compares equal to the stored object and every
	// reconcile issues a no-op update (observed as permanent write churn).
	cluster := brokerSetTestCluster()
	scheme := brokerSetTestScheme(t)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "rp", Namespace: "test"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{ConfigMapHashAnnotationKey: testCurrentChecksum},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name:  "configurator",
						Ports: []corev1.ContainerPort{{Name: "hook", ContainerPort: 8080}},
					}},
					Containers: []corev1.Container{{
						Name: "redpanda",
						Ports: []corev1.ContainerPort{
							{Name: "rpc", ContainerPort: 33145},
							{Name: "kafka", ContainerPort: 9092, Protocol: corev1.ProtocolUDP},
						},
					}},
				},
			},
		},
	}
	nodePool := vectorizedv1alpha1.NodePoolSpecWithDeleted{
		NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{Name: "default", Replicas: ptr.To(int32(1))},
	}

	brokers, err := brokersFromStatefulSet(cluster, sts, nodePool, scheme, false)
	require.NoError(t, err)
	require.Len(t, brokers, 1)

	spec := brokers[0].Spec.PodTemplate.Spec
	assert.Equal(t, corev1.ProtocolTCP, spec.InitContainers[0].Ports[0].Protocol)
	assert.Equal(t, corev1.ProtocolTCP, spec.Containers[0].Ports[0].Protocol)
	// Explicitly-set protocols are preserved.
	assert.Equal(t, corev1.ProtocolUDP, spec.Containers[0].Ports[1].Protocol)
}

func TestReconcileExcessBrokersOneAtATime(t *testing.T) {
	ctx := context.Background()

	get := func(t *testing.T, c k8sclient.Client, name string) *redpandav1alpha2.Broker {
		t.Helper()
		var b redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: name, Namespace: "test"}, &b))
		return &b
	}
	byIndex := func(t *testing.T, c k8sclient.Client) map[int32]*redpandav1alpha2.Broker {
		t.Helper()
		var list redpandav1alpha2.BrokerList
		require.NoError(t, c.List(ctx, &list))
		return indexBrokers(list.Items)
	}

	t.Run("marks exactly one excess broker", func(t *testing.T) {
		// Scale 5→3: only index 4 gets marked in the first pass.
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true},
			{index: 2, podChecksum: testCurrentChecksum, podReady: true},
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
			{index: 4, podChecksum: testCurrentChecksum, podReady: true},
		}, interceptor.Funcs{})

		require.NoError(t, r.reconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, false))
		assert.True(t, get(t, c, "rp-broker-4").Spec.Decommission)
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("in-flight decommission blocks the next mark", func(t *testing.T) {
		// Index 4 is mid-decommission: index 3 must NOT be marked yet.
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
			{index: 4, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum},
		}, interceptor.Funcs{})

		require.NoError(t, r.reconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, true))
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("manual decommission on a desired index blocks scale-down marking", func(t *testing.T) {
		// A human decommissioned index 1 (desired); scale-down of index 3
		// must wait — one disruptive operation at a time.
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
		}, interceptor.Funcs{})

		require.NoError(t, r.reconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, true))
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("decommissioned broker is deleted and the next one marked", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
			{index: 4, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		}, interceptor.Funcs{})

		require.NoError(t, r.reconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, false))

		var gone redpandav1alpha2.Broker
		err := c.Get(ctx, types.NamespacedName{Name: "rp-broker-4", Namespace: "test"}, &gone)
		assert.True(t, apierrors.IsNotFound(err), "decommissioned Broker should be deleted")
		assert.True(t, get(t, c, "rp-broker-3").Spec.Decommission, "next excess broker should be marked")
	})
}
