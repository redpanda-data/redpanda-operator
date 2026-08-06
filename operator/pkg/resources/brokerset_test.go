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
	"github.com/redpanda-data/redpanda-operator/operator/internal/brokerset"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
)

const (
	testCurrentChecksum = "current-checksum"
	testOldChecksum     = "old-checksum"
)

// testTemplateHash returns the pod-template hash of a fixture template of
// the given generation. The hash covers only the pod SPEC, so the generation
// marker is baked into a spec field — fixtures need "current vs outdated"
// generations to produce distinct hashes for the grant-staleness paths.
func testTemplateHash(generation string) string {
	tpl := redpandav1alpha2.BrokerPodTemplate{Spec: corev1.PodSpec{Hostname: generation}}
	return tpl.Hash()
}

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
	index int32
	// name overrides the default "rp-broker-<index>" CR name — required when
	// two brokers share an index (a DiskLost ticket and its replacement).
	name        string
	podChecksum string // "" = no pod
	podReady    bool
	// podUnschedulable marks the pod PodScheduled=False/Unschedulable (the
	// disk-loss shape). Mutually exclusive with podReady.
	podUnschedulable bool
	grant            string
	decommission     bool
	phase            redpandav1alpha2.BrokerPhase
	brokerID         *int32
	// diskLost marks the Broker as a dead incarnation (ticket);
	// diskLostReleased additionally sets the index-release checkpoint.
	diskLost         bool
	diskLostReleased bool
}

// buildBrokerSet constructs a BrokerSetResource backed by a fake client
// pre-populated with the cluster, Broker CRs and pods described by brokers.
func buildBrokerSet(t *testing.T, healthy bool, brokers []testBroker, interceptors interceptor.Funcs) (*BrokerSetResource, k8sclient.Client) {
	scheme := brokerSetTestScheme(t)
	cluster := brokerSetTestCluster()

	objs := []k8sclient.Object{cluster}
	clusterLabels := labels.ForCluster(cluster)
	// Fixture brokers carry the pool label so the engine's pool-scoped
	// listing (ensureBrokers) sees them; "default" keeps PodName unsuffixed.
	poolLabels := clusterLabels.WithNodePool("default")
	for _, tb := range brokers {
		name := tb.name
		if name == "" {
			name = fmt.Sprintf("rp-broker-%d", tb.index)
		}
		broker := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: cluster.Namespace,
				Labels:    poolLabels,
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
						redpandav1alpha2.BrokerConfigChecksumAnnotation:  testCurrentChecksum,
						redpandav1alpha2.BrokerPodTemplateHashAnnotation: testTemplateHash(testCurrentChecksum),
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
		if tb.diskLost || tb.diskLostReleased {
			broker.Status.DiskLost = &redpandav1alpha2.DiskLostStatus{
				At:                metav1.Now(),
				ResourcesReleased: tb.diskLostReleased,
			}
			if broker.Status.Phase == "" {
				broker.Status.Phase = redpandav1alpha2.BrokerPhaseDiskLost
			}
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
					Labels:    clusterLabels,
					Annotations: map[string]string{
						redpandav1alpha2.BrokerConfigChecksumAnnotation:  tb.podChecksum,
						redpandav1alpha2.BrokerPodTemplateHashAnnotation: testTemplateHash(tb.podChecksum),
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
			if tb.podUnschedulable {
				pod.Status.Phase = corev1.PodPending
				pod.Status.Conditions = []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable"},
				}
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
		nodePool:     vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{Name: "default"}},
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

	err := r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	granted := listGrantedBrokers(t, c)
	require.Equal(t, []string{"rp-broker-0"}, granted)

	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b))
	checksum, deadline, ok := feature.ParseRollGrant(b.Annotations[feature.RollGrant.Key])
	require.True(t, ok)
	assert.Equal(t, testTemplateHash(testCurrentChecksum), checksum)
	assert.True(t, deadline.After(time.Now()))
}

func TestEnsureRollGrantsNoSecondGrantWhileActive(t *testing.T) {
	// Broker 0 holds an unexpired grant (mid-roll, pod deleted); broker 1 is
	// outdated but must NOT be granted.
	activeGrant := feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: activeGrant}, // no pod: mid-rotation
		{index: 1, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	granted := listGrantedBrokers(t, c)
	assert.Equal(t, []string{"rp-broker-0"}, granted)
}

func TestEnsureRollGrantsRevokesOnCompletion(t *testing.T) {
	// Broker 0 finished its roll: pod matches the desired checksum, is ready,
	// and the broker is registered. The grant must be revoked; with nothing
	// left to roll the pass is a no-op.
	activeGrant := feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: activeGrant, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
		{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(1))},
	}, interceptor.Funcs{})

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsRevokesStaleChecksum(t *testing.T) {
	// A grant minted for a previous template generation whose roll is
	// COMPLETE (pod current, ready, registered) is revoked, even though its
	// deadline has not passed.
	staleGrant := feature.FormatRollGrant(testTemplateHash(testOldChecksum), time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: staleGrant, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
	}, interceptor.Funcs{})

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

func TestEnsureRollGrantsRekeysStaleMidRoll(t *testing.T) {
	// A desired-template change lands while a granted roll is still in
	// flight (pod outdated). The grant must be re-keyed IN PLACE — the
	// holder may be half drained, and handing the grant to another broker
	// would strand it in maintenance mode, deadlocking the next drain.
	staleGrant := feature.FormatRollGrant(testTemplateHash(testOldChecksum), time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, grant: staleGrant, podChecksum: testOldChecksum, podReady: true, brokerID: ptr.To(int32(0))},
		{index: 1, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	// Still exactly one grant, still on broker 0, now keyed to the current
	// template hash with a fresh deadline.
	require.Equal(t, []string{"rp-broker-0"}, listGrantedBrokers(t, c))
	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b))
	checksum, deadline, ok := feature.ParseRollGrant(b.Annotations[feature.RollGrant.Key])
	require.True(t, ok)
	assert.Equal(t, testTemplateHash(testCurrentChecksum), checksum)
	assert.True(t, deadline.After(time.Now()))
}

func TestEnsureRollGrantsRegrantsExpired(t *testing.T) {
	// Broker 1 holds an EXPIRED grant on an unfinished roll: it is re-granted
	// with priority over broker 0, which is also outdated.
	expiredGrant := feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(-time.Minute))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
		{index: 1, grant: expiredGrant, podChecksum: testOldChecksum, podReady: true},
	}, interceptor.Funcs{})

	err := r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log)
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

	err := r.core(ctrl.Log).EnsureRollGrants(ctx, ctrl.Log)
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

	require.NoError(t, r.core(ctrl.Log).UpdateBroker(ctx, ctrl.Log, &existing, desired))

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

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log))

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

	err := r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log)
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

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log))
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

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(context.Background(), ctrl.Log))
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

	// First pass: Broker CRs cleaned up and the StatefulSet restored, but the
	// pods are not yet re-adopted by it — rollback must hold the success
	// report AND the backup ConfigMap until they are.
	err = RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue, "expected a requeue while the StatefulSet has not adopted the pods")

	var restored appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &restored))
	assert.Equal(t, "backup", restored.Labels["from"], "STS must come from the backup, not a re-render")
	assert.Equal(t, ptr.To(int32(1)), restored.Spec.Replicas)
	owner := metav1.GetControllerOf(&restored)
	require.NotNil(t, owner)
	assert.Equal(t, r.pandaCluster.Name, owner.Name)

	var kept corev1.ConfigMap
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-migration-backup", Namespace: "test"}, &kept),
		"backup ConfigMap must survive until the pods are adopted")

	// Simulate the StatefulSet controller adopting the pod, then finish.
	var pod corev1.Pod
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-0", Namespace: "test"}, &pod))
	require.NoError(t, controllerutil.SetControllerReference(&restored, &pod, r.scheme))
	require.NoError(t, c.Update(ctx, &pod))

	require.NoError(t, RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log))

	var gone corev1.ConfigMap
	err = c.Get(ctx, types.NamespacedName{Name: "rp-migration-backup", Namespace: "test"}, &gone)
	assert.True(t, apierrors.IsNotFound(err), "backup ConfigMap should be deleted once the pods are adopted")

	var persisted vectorizedv1alpha1.Cluster
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp", Namespace: "test"}, &persisted))
	cond := persisted.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
	require.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
	assert.Equal(t, brokerset.MigrationReasonRolledBack, cond.Reason)
}

// TestRollbackStripsFinalizerFromTerminatingBroker covers rollback with a
// degraded Broker controller: normally the controller removes its own
// finalizer via reconcileDelete once the CR is deleted (the pod is no longer
// broker-owned), but when it is down the CR would stay terminating forever.
// Rollback strips the finalizer itself — only on terminating CRs, where the
// controller can never re-add it, so this cannot re-enter the finalizer
// tug-of-war that stalled live rollbacks.
func TestRollbackStripsFinalizerFromTerminatingBroker(t *testing.T) {
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
	}, interceptor.Funcs{})

	var b redpandav1alpha2.Broker
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b))
	b.Finalizers = append(b.Finalizers, brokerset.BrokerDecommissionFinalizer)
	require.NoError(t, c.Update(ctx, &b))

	// Pass 1 deletes the CR; with no live Broker controller the finalizer
	// keeps it terminating.
	require.NoError(t, RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log))
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b))
	require.False(t, b.DeletionTimestamp.IsZero(), "CR should be terminating, held by its finalizer")

	// Pass 2 finds the terminating CR and strips the finalizer.
	require.NoError(t, RollbackBrokerCRs(ctx, c, r.scheme, r.pandaCluster, ctrl.Log))
	err := c.Get(ctx, types.NamespacedName{Name: "rp-broker-0", Namespace: "test"}, &b)
	require.True(t, apierrors.IsNotFound(err), "CR should be gone once the finalizer is stripped")
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

	require.NoError(t, r.core(ctrl.Log).UpdateBroker(context.Background(), ctrl.Log, &existing, desired))
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
		require.NoError(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired))
	})

	t.Run("blocked while restarting", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		cluster.Status.SetRestarting(true)
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "restarting")
	})

	t.Run("blocked while decommissioning", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		cluster.Status.DecommissioningNode = ptr.To(int32(2))
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "decommission")
	})

	t.Run("blocked while STS rollout incomplete", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.live.Status.ReadyReplicas = 2
		r := buildMigrationBrokerSet(t, true, cluster, fix)

		// Pool reports accumulate; the cluster controller flushes ONE
		// aggregate after all pools reconcile. Wire the pool reporter and
		// flush here to assert the blockage reaches the Cluster's
		// BrokerMigration condition through that path.
		agg := brokerset.NewMigrationAggregator()
		r.reporter = NewMigrationPoolReporter(agg, r.nodePool.Name, r.Client, cluster, ctrl.Log)

		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "rollout has not completed")
		FlushBrokerMigrationCondition(ctx, r.Client, cluster, ctrl.Log, agg)

		var persisted vectorizedv1alpha1.Cluster
		require.NoError(t, r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &persisted))
		cond := persisted.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
		require.NotNil(t, cond)
		assert.Equal(t, corev1.ConditionFalse, cond.Status)
		assert.Equal(t, brokerset.MigrationReasonBlocked, cond.Reason)
		assert.Contains(t, cond.Message, "nodepool "+r.nodePool.Name+":")
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
		require.NoError(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired))
	})

	t.Run("blocked while config change pending on STS", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.desired.Spec.Template.Annotations[ConfigMapHashAnnotationKey] = "next-checksum"
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "config change pending")
	})

	t.Run("blocked while a pod runs stale config", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.pods[1].Annotations[ConfigMapHashAnnotationKey] = testOldChecksum
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "not running the desired configuration")
	})

	t.Run("blocked while a pod is not ready", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		fix.pods[2].Status.Conditions = nil
		r := buildMigrationBrokerSet(t, true, cluster, fix)
		requireMigrationBlocked(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), "not ready")
	})

	t.Run("blocked while cluster unhealthy", func(t *testing.T) {
		cluster := brokerSetTestCluster()
		fix := quiescentMigrationFixture(cluster, 3)
		r := buildMigrationBrokerSet(t, false, cluster, fix)
		var requeue *RequeueAfterError
		require.ErrorAs(t, r.core(ctrl.Log).VerifyMigrationPreconditions(ctx, ctrl.Log, fix.live, fix.desired), &requeue)
	})
}

// TestEnsureDeletedPoolWaitsForTerminatingStatefulSet covers the window where
// a pool was removed from spec.nodePools while its StatefulSet is still
// terminating: Ensure must neither converge the StatefulSet (it is going
// away) nor enter the engine's drain path (the engine would find the
// still-listed StatefulSet and run the migration state machine with no
// desired spec to feed it). It waits the termination out instead.
func TestEnsureDeletedPoolWaitsForTerminatingStatefulSet(t *testing.T) {
	scheme := brokerSetTestScheme(t)
	cluster := brokerSetTestCluster()
	pool := vectorizedv1alpha1.NodePoolSpecWithDeleted{
		NodePoolSpec: vectorizedv1alpha1.NodePoolSpec{Name: "blue", Replicas: ptr.To(int32(0))},
		Deleted:      true,
	}

	terminating := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              fmt.Sprintf("%s-%s", cluster.Name, pool.Name),
			Namespace:         cluster.Namespace,
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{"kubernetes.io/test-blocker"},
		},
	}
	broker := &redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp-blue-broker-0",
			Namespace: cluster.Namespace,
			Labels:    labels.ForCluster(cluster).WithNodePool(pool.Name),
		},
		Spec: redpandav1alpha2.BrokerSpec{
			ClusterRef: redpandav1alpha2.ClusterRef{
				Group: ptr.To("redpanda.vectorized.io"),
				Kind:  ptr.To("Cluster"),
				Name:  cluster.Name,
			},
			NetworkIndex: ptr.To(int32(0)),
		},
	}
	require.NoError(t, controllerutil.SetControllerReference(cluster, broker, scheme))

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, terminating, broker).
		Build()

	r := &BrokerSetResource{
		Client:       c,
		scheme:       scheme,
		pandaCluster: cluster,
		stsResource:  &StatefulSetResource{Client: c, pandaCluster: cluster, nodePool: pool, logger: ctrl.Log.WithName("test")},
		nodePool:     pool,
		logger:       ctrl.Log.WithName("test"),
	}

	err := r.Ensure(context.Background())
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue, "expected a requeue while the StatefulSet terminates, got: %v", err)

	// The pool's broker must be untouched: no drain-decommission stamped.
	var got redpandav1alpha2.Broker
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: broker.Name, Namespace: broker.Namespace}, &got))
	require.False(t, got.Spec.Decommission, "drain must not start while the StatefulSet still exists")
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
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true},
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
	})

	t.Run("blocked by in-flight decommission", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum},
		})
		requireMigrationBlocked(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers), "decommissioning")
	})

	t.Run("fully decommissioned broker does not block", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
	})

	t.Run("blocked by active roll-grant", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true, grant: feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(feature.RollGrantTTL))},
		})
		requireMigrationBlocked(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers), "roll-grant")
	})

	t.Run("expired roll-grant does not block", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true, grant: feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(-time.Minute))},
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
	})

	// A missing pod without an unexpired grant is an ABANDONED rotation (or a
	// manual deletion) and must NOT block: the restored StatefulSet recreates
	// the pod, whereas waiting would deadlock on a wedged Broker controller.
	t.Run("missing pod without grant does not block", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0}, // no pod, no grant
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
	})

	t.Run("missing pod with unexpired grant still blocks", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, grant: feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(feature.RollGrantTTL))}, // rotation actively in flight
		})
		requireMigrationBlocked(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers), "roll-grant")
	})

	t.Run("disk-lost ticket mid-decommission does not block", func(t *testing.T) {
		// A ticket's dead-id decommission may be unfinishable (insufficient
		// survivors); blocking the escape hatch on it would wedge rollback.
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, name: "rp-ticket-1", diskLostReleased: true, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, brokerID: ptr.To(int32(1))},
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
	})

	t.Run("unmarked disk-lost ticket does not block", func(t *testing.T) {
		brokers, _ := build(t, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true},
			{index: 1, name: "rp-ticket-1", diskLost: true, brokerID: ptr.To(int32(1))},
		})
		require.NoError(t, brokerset.VerifyRollbackPreconditions(ctrl.Log, brokers))
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

	require.NoError(t, r.core(ctrl.Log).UpdateBroker(context.Background(), ctrl.Log, &existing, desired))

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

		require.NoError(t, r.core(ctrl.Log).EnsureDesiredBroker(ctx, ctrl.Log, &existing, desired))

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

		require.NoError(t, r.core(ctrl.Log).EnsureDesiredBroker(ctx, ctrl.Log, &existing, desired))

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

		require.NoError(t, r.core(ctrl.Log).UpdateBroker(ctx, ctrl.Log, &existing, desired))

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

	require.NoError(t, r.core(ctrl.Log).UpdateBroker(context.Background(), ctrl.Log, &existing, desired))

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

			r := &BrokerSetResource{pandaCluster: cluster, scheme: scheme, nodePool: nodePool, logger: ctrl.Log}
			brokers, err := r.core(ctrl.Log).RenderBrokers(sts, ptr.Deref(nodePool.Replicas, 0), false)
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

	r := &BrokerSetResource{pandaCluster: cluster, scheme: scheme, nodePool: nodePool, logger: ctrl.Log}
	brokers, err := r.core(ctrl.Log).RenderBrokers(sts, ptr.Deref(nodePool.Replicas, 0), false)
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
		return brokerset.IndexBrokers(list.Items)
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

		require.NoError(t, r.core(ctrl.Log).ReconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, false))
		assert.True(t, get(t, c, "rp-broker-4").Spec.Decommission)
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("in-flight decommission blocks the next mark", func(t *testing.T) {
		// Index 4 is mid-decommission: index 3 must NOT be marked yet.
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
			{index: 4, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioning, podChecksum: testCurrentChecksum},
		}, interceptor.Funcs{})

		require.NoError(t, r.core(ctrl.Log).ReconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, true))
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("manual decommission on a desired index blocks scale-down marking", func(t *testing.T) {
		// A human decommissioned index 1 (desired); scale-down of index 3
		// must wait — one disruptive operation at a time.
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
		}, interceptor.Funcs{})

		require.NoError(t, r.core(ctrl.Log).ReconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, true))
		assert.False(t, get(t, c, "rp-broker-3").Spec.Decommission)
	})

	t.Run("decommissioned broker is deleted and the next one marked", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 3, podChecksum: testCurrentChecksum, podReady: true},
			{index: 4, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		}, interceptor.Funcs{})

		require.NoError(t, r.core(ctrl.Log).ReconcileExcessBrokers(ctx, ctrl.Log, byIndex(t, c), 3, false))

		var gone redpandav1alpha2.Broker
		err := c.Get(ctx, types.NamespacedName{Name: "rp-broker-4", Namespace: "test"}, &gone)
		assert.True(t, apierrors.IsNotFound(err), "decommissioned Broker should be deleted")
		assert.True(t, get(t, c, "rp-broker-3").Spec.Decommission, "next excess broker should be marked")
	})
}

// TestDiskLostTicketLifecycle drives ReconcileDiskLostTickets through its
// four outcomes: decommissioned tickets are deleted, unregistered tickets
// are deleted without a decommission, desired-index tickets wait for the
// replacement to REGISTER before the dead id's decommission is marked, and
// undesired-index tickets are marked immediately.
func TestDiskLostTicketLifecycle(t *testing.T) {
	ctx := context.Background()

	partition := func(t *testing.T, c k8sclient.Client) ([]*redpandav1alpha2.Broker, map[int32]*redpandav1alpha2.Broker) {
		t.Helper()
		var list redpandav1alpha2.BrokerList
		require.NoError(t, c.List(ctx, &list))
		live, tickets := brokerset.PartitionDiskLostTickets(list.Items)
		return tickets, brokerset.IndexBrokers(live)
	}
	getBroker := func(t *testing.T, c k8sclient.Client, name string) *redpandav1alpha2.Broker {
		t.Helper()
		var b redpandav1alpha2.Broker
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: name, Namespace: "test"}, &b))
		return &b
	}

	t.Run("waits for the replacement to register", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLostReleased: true, brokerID: ptr.To(int32(1))},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true}, // replacement, unregistered
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		_, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, false)
		require.NoError(t, err)
		assert.False(t, getBroker(t, c, "rp-ticket-1").Spec.Decommission,
			"the dead id must not be decommissioned before the replacement registers")
	})

	t.Run("marks once the replacement registered", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLostReleased: true, brokerID: ptr.To(int32(1))},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(5))},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		inFlight, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, false)
		require.NoError(t, err)
		assert.True(t, inFlight, "the mark must propagate to the excess path")
		assert.True(t, getBroker(t, c, "rp-ticket-1").Spec.Decommission)
	})

	t.Run("in-flight decommission blocks the mark", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLostReleased: true, brokerID: ptr.To(int32(1))},
			{index: 1, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(5))},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		_, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, true)
		require.NoError(t, err)
		assert.False(t, getBroker(t, c, "rp-ticket-1").Spec.Decommission)
	})

	t.Run("undesired index marks immediately", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 2, name: "rp-ticket-2", diskLostReleased: true, brokerID: ptr.To(int32(2))},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		_, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 1, false)
		require.NoError(t, err)
		assert.True(t, getBroker(t, c, "rp-ticket-2").Spec.Decommission,
			"no replacement will come for an undesired index")
	})

	t.Run("unreleased ticket is left alone", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLost: true, brokerID: ptr.To(int32(1))},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		_, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, false)
		require.NoError(t, err)
		ticket := getBroker(t, c, "rp-ticket-1")
		assert.False(t, ticket.Spec.Decommission)
		assert.NotNil(t, ticket.Status.DiskLost)
	})

	t.Run("decommissioned ticket is deleted", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLostReleased: true, decommission: true, phase: redpandav1alpha2.BrokerPhaseDecommissioned},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		_, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, false)
		require.NoError(t, err)
		var gone redpandav1alpha2.Broker
		assert.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "rp-ticket-1", Namespace: "test"}, &gone)))
	})

	t.Run("released ticket without a recorded id is deleted, never marked", func(t *testing.T) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 1, name: "rp-ticket-1", diskLostReleased: true},
		}, interceptor.Funcs{})

		tickets, liveByIndex := partition(t, c)
		inFlight, err := r.core(ctrl.Log).ReconcileDiskLostTickets(ctx, ctrl.Log, tickets, liveByIndex, 2, false)
		require.NoError(t, err)
		assert.False(t, inFlight)
		var gone redpandav1alpha2.Broker
		assert.True(t, apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Name: "rp-ticket-1", Namespace: "test"}, &gone)))
	})
}

// TestDiskLostTicketReleasesIndex proves the index handover end to end
// through the steady-state Ensure: a RELEASED ticket no longer occupies its
// network index, so the ordinary missing-index path creates the replacement
// under the same index; an UNRELEASED ticket still pins it.
func TestDiskLostTicketReleasesIndex(t *testing.T) {
	ctx := context.Background()

	run := func(t *testing.T, released bool) (map[int32]*redpandav1alpha2.Broker, []*redpandav1alpha2.Broker) {
		r, c := buildBrokerSet(t, true, []testBroker{
			{index: 0, podChecksum: testCurrentChecksum, podReady: true, brokerID: ptr.To(int32(0))},
			{index: 1, name: "rp-ticket-1", diskLost: !released, diskLostReleased: released, brokerID: ptr.To(int32(1))},
		}, interceptor.Funcs{})

		fix := quiescentMigrationFixture(brokerSetTestCluster(), 2)
		stsKey := types.NamespacedName{Name: fix.live.Name, Namespace: fix.live.Namespace}
		if err := r.core(ctrl.Log).Ensure(ctx, stsKey, fix.desired, 2); err != nil {
			// The spec sync makes the fixture pod legitimately outdated, so
			// grant issuance may requeue — irrelevant to index handling.
			var requeue *RequeueAfterError
			require.ErrorAs(t, err, &requeue)
		}

		var list redpandav1alpha2.BrokerList
		require.NoError(t, c.List(ctx, &list))
		live, tickets := brokerset.PartitionDiskLostTickets(list.Items)
		return brokerset.IndexBrokers(live), tickets
	}

	t.Run("released ticket frees the index for a replacement", func(t *testing.T) {
		liveByIndex, tickets := run(t, true)
		require.NotNil(t, liveByIndex[1], "a replacement must be created at the released index")
		assert.NotEqual(t, "rp-ticket-1", liveByIndex[1].Name)
		require.Len(t, tickets, 1, "the ticket lingers as the decommission record")
	})

	t.Run("unreleased ticket pins the index", func(t *testing.T) {
		liveByIndex, _ := run(t, false)
		assert.Nil(t, liveByIndex[1], "no replacement while pod/PVC names are not free")
	})
}

// TestEnsureRollGrantsSkipsDiskLostTickets: a ticket is never a roll
// candidate, and a grant stranded on it (node died mid-roll) is revoked so
// it cannot serialize the fleet.
func TestEnsureRollGrantsSkipsDiskLostTickets(t *testing.T) {
	ctx := context.Background()
	activeGrant := feature.FormatRollGrant(testTemplateHash(testCurrentChecksum), time.Now().Add(feature.RollGrantTTL))
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testOldChecksum, podReady: true},
		{index: 1, name: "rp-ticket-1", diskLostReleased: true, brokerID: ptr.To(int32(1)), grant: activeGrant},
	}, interceptor.Funcs{})

	err := r.core(ctrl.Log).EnsureRollGrants(ctx, ctrl.Log)
	var requeue *RequeueAfterError
	require.ErrorAs(t, err, &requeue)

	// The stranded grant is revoked and the rotation grant lands on the
	// ordinary outdated broker — the ticket's stale grant did not count as
	// an active roll.
	assert.Equal(t, []string{"rp-broker-0"}, listGrantedBrokers(t, c))
}

// TestEnsureRollGrantsStuckIsNotACandidate: grants are rotation-only — an
// unschedulable-Stuck broker (current template) is never granted; disk loss
// is handled by the DiskLost replacement flow instead.
func TestEnsureRollGrantsStuckIsNotACandidate(t *testing.T) {
	ctx := context.Background()
	r, c := buildBrokerSet(t, true, []testBroker{
		{index: 0, podChecksum: testCurrentChecksum, podReady: true},
		{index: 1, podChecksum: testCurrentChecksum, podUnschedulable: true, phase: redpandav1alpha2.BrokerPhaseStuck, brokerID: ptr.To(int32(1))},
	}, interceptor.Funcs{})

	require.NoError(t, r.core(ctrl.Log).EnsureRollGrants(ctx, ctrl.Log))
	assert.Empty(t, listGrantedBrokers(t, c))
}

// TestMigrationPrunesStaleShadows covers a scale-down landing between shadow
// creation and handover: the StatefulSet machinery (which keeps running while
// the live STS exists) removed the ordinal's broker, so its shadow is stale
// and must be pruned before the destructive step — surviving into steady
// state it would read as an excess broker to decommission, a decommission
// that can never complete.
func TestMigrationPrunesStaleShadows(t *testing.T) {
	ctx := context.Background()
	cluster := brokerSetTestCluster()

	// Live STS at 2 replicas, quiescent and healthy — but shadows exist for
	// ordinals 0..2 from before a 3→2 scale-down.
	fix := quiescentMigrationFixture(cluster, 2)
	r := buildMigrationBrokerSet(t, true, cluster, fix)

	poolLabels := labels.ForCluster(cluster).WithNodePool("default")
	for i := int32(0); i < 3; i++ {
		shadow := &redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("rp-shadow-%d", i),
				Namespace: cluster.Namespace,
				Labels:    poolLabels,
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef: redpandav1alpha2.ClusterRef{
					Group: ptr.To("redpanda.vectorized.io"),
					Kind:  ptr.To("Cluster"),
					Name:  cluster.Name,
				},
				NetworkIndex: ptr.To(i),
			},
		}
		require.NoError(t, controllerutil.SetControllerReference(cluster, shadow, r.scheme))
		require.NoError(t, r.Client.Create(ctx, shadow))
	}

	stsKey := types.NamespacedName{Name: fix.live.Name, Namespace: fix.live.Namespace}
	require.NoError(t, r.core(ctrl.Log).Ensure(ctx, stsKey, fix.desired, 2))

	// The stale shadow is pruned, without decommission intent.
	var gone redpandav1alpha2.Broker
	err := r.Client.Get(ctx, types.NamespacedName{Name: "rp-shadow-2", Namespace: cluster.Namespace}, &gone)
	assert.True(t, apierrors.IsNotFound(err), "stale shadow above the live replica count should be pruned")

	// The needed shadows survive untouched and the migration proceeded to
	// the handover: the StatefulSet is orphan-deleted.
	for i := 0; i < 2; i++ {
		var b redpandav1alpha2.Broker
		require.NoError(t, r.Client.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("rp-shadow-%d", i), Namespace: cluster.Namespace}, &b))
		assert.False(t, b.Spec.Decommission, "needed shadow %d must not carry decommission intent", i)
	}
	var sts appsv1.StatefulSet
	err = r.Client.Get(ctx, stsKey, &sts)
	assert.True(t, apierrors.IsNotFound(err), "migration should have orphan-deleted the StatefulSet")
}

// TestMigrationRefreshesStaleBackup covers a leaked backup ConfigMap (e.g.
// from a rollback aborted in its post-delete window) being refreshed by the
// next migration: rollback restores the backup verbatim, so restoring
// anything but the CURRENT StatefulSet would roll the re-adopted pods.
func TestMigrationRefreshesStaleBackup(t *testing.T) {
	ctx := context.Background()
	cluster := brokerSetTestCluster()

	fix := quiescentMigrationFixture(cluster, 2)
	r := buildMigrationBrokerSet(t, true, cluster, fix)

	stale := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-migration-backup", cluster.Name),
			Namespace: cluster.Namespace,
		},
		Data: map[string]string{
			"default.json": `{"metadata":{"name":"rp"},"spec":{"replicas":9}}`,
		},
	}
	require.NoError(t, r.Client.Create(ctx, stale))

	stsKey := types.NamespacedName{Name: fix.live.Name, Namespace: fix.live.Namespace}
	require.NoError(t, r.core(ctrl.Log).Ensure(ctx, stsKey, fix.desired, 2))

	var cm corev1.ConfigMap
	require.NoError(t, r.Client.Get(ctx, k8sclient.ObjectKeyFromObject(stale), &cm))

	var backup appsv1.StatefulSet
	require.NoError(t, json.Unmarshal([]byte(cm.Data["default.json"]), &backup))
	require.Equal(t, int32(2), *backup.Spec.Replicas, "the backup must reflect the live StatefulSet, not the leaked entry")
	require.Equal(t, testCurrentChecksum, backup.Spec.Template.Annotations[ConfigMapHashAnnotationKey])
}
