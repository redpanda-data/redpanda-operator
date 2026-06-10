// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pvcunbinder

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	operatorlabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

func newScheme(t *testing.T, withV2, withStretch, withV1 bool) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	if withV2 || withStretch {
		require.NoError(t, redpandav1alpha2.Install(s))
	}
	if withV1 {
		require.NoError(t, vectorizedv1alpha1.Install(s))
	}
	return s
}

func newController(t *testing.T, s *runtime.Scheme, objs ...client.Object) *Controller {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	// Reader is left nil — the Controller falls back to Client, so the
	// fake client serves both the cached and "uncached" roles in tests.
	return &Controller{
		Client: c,
	}
}

func newPod(name, namespace, instance string) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				// Every pod created via this helper is treated as
				// operator-managed via the v1 label
				// (`managed-by=redpanda-operator`). Tests can:
				//   - clear ManagedByKey to model an unrelated workload, or
				//   - replace with the chart's broker label
				//     (`cluster.redpanda.com/broker=true`) to exercise
				//     Gate 2's second LIST.
				operatorlabels.ManagedByKey: "redpanda-operator",
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       "sts-" + name,
				Controller: ptr.To(true),
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	if instance != "" {
		p.Labels[operatorlabels.InstanceKey] = instance
	}
	return p
}

func podWithVolumeAffinityFailure(name, namespace, instance string) *corev1.Pod {
	p := newPod(name, namespace, instance)
	p.Status.Conditions = []corev1.PodCondition{{
		Type:    corev1.PodScheduled,
		Status:  corev1.ConditionFalse,
		Reason:  "Unschedulable",
		Message: "0/3 nodes are available: 3 node(s) had volume node affinity conflict.",
	}}
	return p
}

func newPVC(name, namespace, instance, volumeName string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{operatorlabels.InstanceKey: instance},
		},
		Spec: corev1.PersistentVolumeClaimSpec{VolumeName: volumeName},
	}
}

// pvWithAnnotations builds a PV in the given phase carrying the given
// annotations, pinned to `hostname` when non-empty. Used to exercise
// the durable PV gates (Gates 0 and 4).
func pvWithAnnotations(name string, phase corev1.PersistentVolumePhase, hostname string, annotations map[string]string) *corev1.PersistentVolume {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
		Status:     corev1.PersistentVolumeStatus{Phase: phase},
	}
	if hostname != "" {
		pv.Spec.NodeAffinity = &corev1.VolumeNodeAffinity{
			Required: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{{
						Key:      corev1.LabelHostname,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{hostname},
					}},
				}},
			},
		}
	}
	return pv
}

func newNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// TestCheckPVGates exercises the durable, uncached PV-annotation gates
// that replaced the in-memory tracker. Gate 0 (unbindInFlight) holds
// while a PV's recorded claim hasn't been observed recreated (new UID)
// and bound; Gate 4 (freedPVUnresolved) holds while a freed PV is a
// live rebinding candidate. Both must survive "restarts" by
// construction — there is no process state, so every subtest starting
// from bare API objects IS the restart case.
func TestCheckPVGates(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)
	const key = "/ns/redpanda"

	inFlightAnns := func(claim string) map[string]string {
		return map[string]string{
			InFlightAnnotation:      key,
			InFlightClaimAnnotation: claim,
		}
	}

	// otherPod is a sibling broker in the same cluster whose claims
	// never overlap the in-flight annotations used below — the default
	// perspective from which the gates are evaluated.
	otherPod := func() *corev1.Pod {
		return withPVC(newPod("rp-9", "ns", "redpanda"), "datadir-rp-9")
	}

	t.Run("empty cluster key engages no gates", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeAvailable, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, "", otherPod())
		require.NoError(t, err)
		require.False(t, state.unbindInFlight)
		require.False(t, state.freedPVUnresolved)
	})

	t.Run("no annotated PVs engages no gates", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeBound, "node-a", nil)
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.False(t, state.unbindInFlight)
		require.False(t, state.freedPVUnresolved)
	})

	t.Run("annotations for a different cluster are ignored", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, "/other-ns/other", otherPod())
		require.NoError(t, err)
		require.False(t, state.unbindInFlight)
	})

	t.Run("in-flight: claim deleted but not recreated holds the gate", func(t *testing.T) {
		// The restart-mid-unbind case: PVC deleted, pod deleted,
		// operator restarted. No PVC object exists yet.
		pv := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.unbindInFlight)
	})

	t.Run("in-flight: old claim Terminating holds the gate for siblings", func(t *testing.T) {
		// Deleted PVC held in Terminating by the pvc-protection
		// finalizer (its pod hasn't been deleted yet).
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc.UID = "uid-old"
		pvc.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
		pvc.Finalizers = []string{"kubernetes.io/pvc-protection"}
		pv := pvWithAnnotations("pv-0", corev1.VolumeBound, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv, pvc)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.unbindInFlight)
	})

	t.Run("in-flight: pod's OWN Terminating claim does not block its retry (deadlock guard)", func(t *testing.T) {
		// The pod-delete-failed case: the claim is stuck Terminating
		// because pvc-protection waits for the pod, and only THIS
		// pod's reconcile can complete the unbind by deleting the pod.
		// Blocking it would deadlock the cluster's unbinder.
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc.UID = "uid-old"
		pvc.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
		pvc.Finalizers = []string{"kubernetes.io/pvc-protection"}
		pv := pvWithAnnotations("pv-0", corev1.VolumeBound, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		owner := withPVC(newPod("rp-0", "ns", "redpanda"), "datadir-rp-0")
		r := newController(t, s, pv, pvc)
		state, err := r.checkPVGates(ctx, key, owner)
		require.NoError(t, err)
		require.False(t, state.unbindInFlight, "a pod must be allowed to finish its own unbind")
	})

	t.Run("in-flight: intact old claim settles and clears annotations (deadlock guard)", func(t *testing.T) {
		// The delete-never-happened case: a previous reconcile wrote
		// the annotation, then failed before the PVC delete. The old
		// claim is alive, bound, and not Terminating — the pre-unbind
		// state. Holding the gate here would deadlock (the retry that
		// would delete the claim is the thing being deferred).
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc.UID = "uid-old"
		pv := pvWithAnnotations("pv-0", corev1.VolumeBound, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv, pvc)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.False(t, state.unbindInFlight)

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.NotContains(t, got.Annotations, InFlightAnnotation)
	})

	t.Run("in-flight: recreated claim with new UID but unbound holds the gate", func(t *testing.T) {
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "")
		pvc.UID = "uid-new"
		pv := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv, pvc)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.unbindInFlight)
	})

	t.Run("in-flight: recreated claim bound with new UID settles and clears annotations", func(t *testing.T) {
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "pv-new")
		pvc.UID = "uid-new"
		pv := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		r := newController(t, s, pv, pvc)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.False(t, state.unbindInFlight)

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.NotContains(t, got.Annotations, InFlightAnnotation)
		require.NotContains(t, got.Annotations, InFlightClaimAnnotation)
	})

	t.Run("in-flight: malformed claim annotation holds the gate (conservative)", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("garbage"))
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.unbindInFlight)
	})

	t.Run("freed: Available with live node holds the gate", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeAvailable, "node-a", map[string]string{FreedPVAnnotation: key})
		r := newController(t, s, pv, newNode("node-a"))
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.freedPVUnresolved)
	})

	t.Run("freed: Available with node permanently gone does not hold, keeps annotation", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeAvailable, "node-a", map[string]string{FreedPVAnnotation: key})
		r := newController(t, s, pv) // no Node object
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.False(t, state.freedPVUnresolved)

		// Annotation kept: node-name reuse would make the PV a live
		// candidate again and the gate must be able to re-engage.
		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.Contains(t, got.Annotations, FreedPVAnnotation)
	})

	t.Run("freed: re-Bound clears the annotation and does not hold", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeBound, "node-a", map[string]string{FreedPVAnnotation: key})
		r := newController(t, s, pv, newNode("node-a"))
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.False(t, state.freedPVUnresolved)

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.NotContains(t, got.Annotations, FreedPVAnnotation)
	})

	t.Run("freed: Available with unresolvable affinity holds the gate (conservative)", func(t *testing.T) {
		pv := pvWithAnnotations("pv-0", corev1.VolumeAvailable, "", map[string]string{FreedPVAnnotation: key})
		r := newController(t, s, pv)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.freedPVUnresolved)
	})

	t.Run("both gates evaluated in one pass", func(t *testing.T) {
		inflight := pvWithAnnotations("pv-0", corev1.VolumeReleased, "node-a", inFlightAnns("ns/datadir-rp-0/uid-old"))
		freed := pvWithAnnotations("pv-1", corev1.VolumeAvailable, "node-b", map[string]string{FreedPVAnnotation: key})
		r := newController(t, s, inflight, freed, newNode("node-b"))
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.unbindInFlight)
		require.True(t, state.freedPVUnresolved)
	})
}

// TestPrepareForUnbind verifies the pre-deletion patch that makes the
// in-flight gate durable: Retain policy + both in-flight annotations
// (cluster key and claim namespace/name/uid) must land in one call,
// BEFORE the PVC delete that follows.
func TestPrepareForUnbind(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)
	const key = "/ns/redpanda"

	boundPV := func() *corev1.PersistentVolume {
		return &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-0"},
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
				ClaimRef: &corev1.ObjectReference{
					Namespace: "ns",
					Name:      "datadir-rp-0",
					UID:       "uid-old",
				},
			},
			Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
		}
	}

	t.Run("sets retain policy and in-flight annotations", func(t *testing.T) {
		pv := boundPV()
		r := newController(t, s, pv)
		require.NoError(t, r.prepareForUnbind(ctx, pv, key))

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.Equal(t, corev1.PersistentVolumeReclaimRetain, got.Spec.PersistentVolumeReclaimPolicy)
		require.Equal(t, key, got.Annotations[InFlightAnnotation])
		require.Equal(t, "ns/datadir-rp-0/uid-old", got.Annotations[InFlightClaimAnnotation])
	})

	t.Run("empty cluster key only sets retain policy", func(t *testing.T) {
		pv := boundPV()
		r := newController(t, s, pv)
		require.NoError(t, r.prepareForUnbind(ctx, pv, ""))

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.Equal(t, corev1.PersistentVolumeReclaimRetain, got.Spec.PersistentVolumeReclaimPolicy)
		require.NotContains(t, got.Annotations, InFlightAnnotation)
	})

	t.Run("idempotent when already prepared", func(t *testing.T) {
		pv := boundPV()
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
		pv.Annotations = map[string]string{
			InFlightAnnotation:      key,
			InFlightClaimAnnotation: "ns/datadir-rp-0/uid-old",
		}
		r := newController(t, s, pv)
		require.NoError(t, r.prepareForUnbind(ctx, pv, key))
	})
}

// TestMaybeRecyclePersistentVolume verifies the rebinding path's write
// side: clearing the ClaimRef must stamp the freed-PV annotation in
// the same patch so Gate 4 can see the floating disk durably.
func TestMaybeRecyclePersistentVolume(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)
	const key = "/ns/redpanda"

	releasedPV := func() *corev1.PersistentVolume {
		return &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-0"},
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/data"},
				},
				ClaimRef: &corev1.ObjectReference{Namespace: "ns", Name: "datadir-rp-0", UID: "uid-old"},
			},
			Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased},
		}
	}

	t.Run("rebinding on clears ClaimRef and stamps freed annotation", func(t *testing.T) {
		pv := releasedPV()
		r := newController(t, s, pv)
		r.AllowRebinding = true
		require.NoError(t, r.maybeRecyclePersistentVolume(ctx, pv, key))

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.Nil(t, got.Spec.ClaimRef)
		require.Equal(t, key, got.Annotations[FreedPVAnnotation])
	})

	t.Run("rebinding off leaves ClaimRef and annotations untouched", func(t *testing.T) {
		pv := releasedPV()
		r := newController(t, s, pv)
		require.NoError(t, r.maybeRecyclePersistentVolume(ctx, pv, key))

		var got corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-0"}, &got))
		require.NotNil(t, got.Spec.ClaimRef)
		require.NotContains(t, got.Annotations, FreedPVAnnotation)
	})
}

// TestClusterKey verifies the key written into the PV gate annotations.
// Different K8s clusters (multicluster mode) must produce distinct keys
// for the same cluster name+namespace, and pods without the instance
// label intentionally produce an empty key to fall back to
// non-serialized behavior.
func TestClusterKey(t *testing.T) {
	cases := []struct {
		name        string
		clusterName string
		pod         *corev1.Pod
		want        string
	}{
		{
			name:        "single-cluster, instance label set",
			clusterName: "",
			pod:         newPod("redpanda-0", "redpanda-ns", "redpanda"),
			want:        "/redpanda-ns/redpanda",
		},
		{
			name:        "multicluster, ClusterName prefix included",
			clusterName: "k8s-cluster-a",
			pod:         newPod("redpanda-0", "redpanda-ns", "redpanda"),
			want:        "k8s-cluster-a/redpanda-ns/redpanda",
		},
		{
			name:        "no instance label returns empty key",
			clusterName: "",
			pod:         newPod("orphan-0", "default", ""),
			want:        "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Controller{ClusterName: tc.clusterName}
			require.Equal(t, tc.want, r.clusterKey(tc.pod))
		})
	}
}

// TestCannotCheckCRType verifies the error categorizer used by the
// pause-annotation lookup. The three "we can't ask about this type"
// categories (NotFound, NoMatch, NotRegistered) must all be classified
// as non-fatal so the reconcile can fall through to other CR types or
// proceed without pause.
func TestCannotCheckCRType(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "NotFound classified as cannot-check",
			err:  apierrors.NewNotFound(schema.GroupResource{Group: "cluster.redpanda.com", Resource: "redpandas"}, "foo"),
			want: true,
		},
		{
			name: "NoMatchError classified as cannot-check",
			err:  &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "cluster.redpanda.com", Kind: "Redpanda"}},
			want: true,
		},
		{
			name: "NotRegisteredErr classified as cannot-check",
			err:  runtime.NewNotRegisteredErrForKind("scheme", schema.GroupVersionKind{Group: "cluster.redpanda.com", Kind: "Redpanda", Version: "v1alpha2"}),
			want: true,
		},
		{
			name: "unrelated error returned as fatal",
			err:  fmt.Errorf("api server timeout"),
			want: false,
		},
		{
			name: "nil treated as fatal (caller should not have called us)",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cannotCheckCRType(tc.err))
		})
	}
}

// TestPodHasVolumeAffinityUnschedulable validates the signature
// matcher used by Gate 2 to count "other pods that look like ours."
// False positives here would cause unnecessary deferrals; false
// negatives would let the unbinder fire during cluster-wide events.
func TestPodHasVolumeAffinityUnschedulable(t *testing.T) {
	cases := []struct {
		name      string
		condition corev1.PodCondition
		want      bool
	}{
		{
			name: "volume node affinity message matches",
			condition: corev1.PodCondition{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Reason:  "Unschedulable",
				Message: "0/3 nodes are available: 3 node(s) had volume node affinity conflict.",
			},
			want: true,
		},
		{
			name: "no nodes available message also matches",
			condition: corev1.PodCondition{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Reason:  "Unschedulable",
				Message: "0/5 nodes are available: insufficient cpu.",
			},
			want: true,
		},
		{
			name: "Unschedulable with unrelated message doesn't match",
			condition: corev1.PodCondition{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionFalse,
				Reason:  "Unschedulable",
				Message: "1 pod has unbound immediate PersistentVolumeClaims",
			},
			want: false,
		},
		{
			name: "PodScheduled=True doesn't match",
			condition: corev1.PodCondition{
				Type:    corev1.PodScheduled,
				Status:  corev1.ConditionTrue,
				Reason:  "",
				Message: "",
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{tc.condition}}}
			require.Equal(t, tc.want, podHasVolumeAffinityUnschedulable(pod))
		})
	}

	t.Run("no PodScheduled condition", func(t *testing.T) {
		require.False(t, podHasVolumeAffinityUnschedulable(&corev1.Pod{}))
	})
}

// TestIsClusterPaused covers the three CR types the unbinder honors
// for the pause annotation and verifies graceful behavior when types
// or CRDs are absent. Multicluster mode (which only has v2 types in
// scheme) is exercised by the "v1 type not in scheme" case.
func TestIsClusterPaused(t *testing.T) {
	ctx := context.Background()

	t.Run("paused via Redpanda v2 CR", func(t *testing.T) {
		s := newScheme(t, true, false, false)
		rp := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "redpanda",
				Namespace:   "ns",
				Annotations: map[string]string{PauseAnnotation: "true"},
			},
		}
		r := newController(t, s, rp)
		paused, err := r.isClusterPaused(ctx, newPod("redpanda-0", "ns", "redpanda"))
		require.NoError(t, err)
		require.True(t, paused)
	})

	t.Run("paused via StretchCluster CR", func(t *testing.T) {
		s := newScheme(t, true, true, false)
		sc := &redpandav1alpha2.StretchCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "stretch",
				Namespace:   "ns",
				Annotations: map[string]string{PauseAnnotation: "true"},
			},
		}
		r := newController(t, s, sc)
		paused, err := r.isClusterPaused(ctx, newPod("stretch-0", "ns", "stretch"))
		require.NoError(t, err)
		require.True(t, paused)
	})

	t.Run("paused via v1 Cluster CR", func(t *testing.T) {
		s := newScheme(t, true, true, true)
		cluster := &vectorizedv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "legacy",
				Namespace:   "ns",
				Annotations: map[string]string{PauseAnnotation: "true"},
			},
		}
		r := newController(t, s, cluster)
		paused, err := r.isClusterPaused(ctx, newPod("legacy-0", "ns", "legacy"))
		require.NoError(t, err)
		require.True(t, paused)
	})

	t.Run("annotation value other than 'true' is not paused", func(t *testing.T) {
		s := newScheme(t, true, false, false)
		rp := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "redpanda",
				Namespace:   "ns",
				Annotations: map[string]string{PauseAnnotation: "yes"},
			},
		}
		r := newController(t, s, rp)
		paused, err := r.isClusterPaused(ctx, newPod("redpanda-0", "ns", "redpanda"))
		require.NoError(t, err)
		require.False(t, paused)
	})

	t.Run("CR exists without annotation is not paused", func(t *testing.T) {
		s := newScheme(t, true, false, false)
		rp := &redpandav1alpha2.Redpanda{ObjectMeta: metav1.ObjectMeta{Name: "redpanda", Namespace: "ns"}}
		r := newController(t, s, rp)
		paused, err := r.isClusterPaused(ctx, newPod("redpanda-0", "ns", "redpanda"))
		require.NoError(t, err)
		require.False(t, paused)
	})

	t.Run("no instance label on Pod returns not paused", func(t *testing.T) {
		s := newScheme(t, true, false, false)
		r := newController(t, s)
		paused, err := r.isClusterPaused(ctx, newPod("orphan-0", "ns", ""))
		require.NoError(t, err)
		require.False(t, paused)
	})

	t.Run("no CR of any type exists is not paused", func(t *testing.T) {
		s := newScheme(t, true, true, true)
		r := newController(t, s)
		paused, err := r.isClusterPaused(ctx, newPod("redpanda-0", "ns", "redpanda"))
		require.NoError(t, err)
		require.False(t, paused)
	})

	t.Run("v1 type not in scheme (multicluster mode) does not error", func(t *testing.T) {
		// Only v2 types in scheme — v1.Cluster Get returns NotRegisteredErr.
		// The function should swallow that and return false (not paused).
		s := newScheme(t, true, false, false)
		r := newController(t, s)
		paused, err := r.isClusterPaused(ctx, newPod("redpanda-0", "ns", "redpanda"))
		require.NoError(t, err)
		require.False(t, paused)
	})
}

// withPVC adds a StatefulSet-style PVC volume to a Pod (claim name ends
// in pod name, matching StsPVCs() suffix-detection).
func withPVC(p *corev1.Pod, claimName string) *corev1.Pod {
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: claimName,
			},
		},
	})
	return p
}

// newPVWithAffinity constructs a PV with a ClaimRef pointing at the
// given PVC and a NodeAffinity pinning it to `hostname` via the
// standard kubernetes.io/hostname label.
func newPVWithAffinity(name, claimNamespace, claimName, hostname string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef: &corev1.ObjectReference{
				Namespace: claimNamespace,
				Name:      claimName,
			},
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      corev1.LabelHostname,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{hostname},
						}},
					}},
				},
			},
		},
	}
}

// TestMultiNodeEventInProgress verifies Gate 2's distinct-node detector.
// The key behavioral property: counting distinct *nodes* affected by
// stuck pods, not distinct pods. Multiple co-tenant pods on the same
// failed node should NOT be classified as a multi-node K8s event.
func TestMultiNodeEventInProgress(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)

	t.Run("no stuck pods returns false", func(t *testing.T) {
		r := newController(t, s)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("single stuck pod on one node returns false", func(t *testing.T) {
		pod := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pvc := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pv := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		r := newController(t, s, pod, pvc, pv)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("two stuck pods on the SAME node returns false (single-node failure)", func(t *testing.T) {
		// Two pods from different Redpanda clusters co-located on
		// node-a. When node-a dies, both go Pending. That's a
		// legitimate single-node failure the unbinder should act on,
		// not a multi-node K8s event.
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns-a", "cluster-a"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rpb-0", "ns-b", "cluster-b"), "datadir-rpb-0")
		pvc0 := newPVC("datadir-rp-0", "ns-a", "cluster-a", "pv-0")
		pvc1 := newPVC("datadir-rpb-0", "ns-b", "cluster-b", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns-a", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns-b", "datadir-rpb-0", "node-a") // same node
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got, "co-tenant pods on the same failed node are NOT a multi-node event")
	})

	t.Run("two stuck pods on DIFFERENT nodes returns true (K8s-wide event)", func(t *testing.T) {
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-rp-1", "ns", "redpanda", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns", "datadir-rp-1", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("stuck pods across different Redpanda clusters on different nodes are caught", func(t *testing.T) {
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns-a", "cluster-a"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rpb-0", "ns-b", "cluster-b"), "datadir-rpb-0")
		pvc0 := newPVC("datadir-rp-0", "ns-a", "cluster-a", "pv-0")
		pvc1 := newPVC("datadir-rpb-0", "ns-b", "cluster-b", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns-a", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns-b", "datadir-rpb-0", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.True(t, got, "Gate 2 is K8s-cluster-wide, not per-Redpanda-cluster")
	})

	t.Run("non-Pending pod ignored", func(t *testing.T) {
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pod1.Status.Phase = corev1.PodRunning
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-rp-1", "ns", "redpanda", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns", "datadir-rp-1", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("non-STS-owned pod ignored", func(t *testing.T) {
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pod1.OwnerReferences = nil
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-rp-1", "ns", "redpanda", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns", "datadir-rp-1", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("pod Pending for non-volume-affinity reason ignored", func(t *testing.T) {
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pod1 := withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pod1.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "1 pod has unbound immediate PersistentVolumeClaims",
		}}
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-rp-1", "ns", "redpanda", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns", "datadir-rp-1", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("unrelated workload stuck on another node is NOT counted (managed-by scope)", func(t *testing.T) {
		// pod0 is operator-managed and stuck on node-a; podOther is a
		// non-operator workload (e.g., a Postgres StatefulSet using
		// local PVs) stuck on node-b. Before the managed-by scope
		// fix, this flipped Gate 2 to "multi-node" and caused silent
		// inaction on legitimate single-node Redpanda failures.
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		podOther := withPVC(podWithVolumeAffinityFailure("postgres-0", "ns", "postgres"), "datadir-postgres-0")
		// Drop the managed-by label that newPod adds — model an
		// unrelated workload.
		delete(podOther.Labels, operatorlabels.ManagedByKey)
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-postgres-0", "ns", "postgres", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns", "datadir-postgres-0", "node-b")
		r := newController(t, s, pod0, podOther, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got, "unrelated workload on a different node must not flip Gate 2")
	})

	t.Run("chart-rendered broker pod (managed-by=Helm + broker=true) IS counted (second LIST)", func(t *testing.T) {
		// Pins the label contract from charts/redpanda/statefulset.go
		// StatefulSetPodLabels: every chart-rendered broker pod —
		// v2 Redpanda, StretchCluster, and direct Helm installs —
		// carries cluster.redpanda.com/broker=true on the pod, while
		// managed-by is "Helm" (NOT redpanda-operator) and the
		// operator's cluster.redpanda.com/operator=v2 ownership label
		// is on the StatefulSet object only, never the pod. Gate 2's
		// second LIST must catch these pods; selecting on operator=v2
		// would match nothing (the regression from PR review).
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns-a", "redpanda-a"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rpb-0", "ns-b", "redpanda-b"), "datadir-rpb-0")
		// pod1 carries exactly the chart-rendered label set.
		delete(pod1.Labels, operatorlabels.ManagedByKey)
		pod1.Labels[operatorlabels.ManagedByKey] = "Helm"
		pod1.Labels[brokerLabelKey] = brokerLabelValue
		pvc0 := newPVC("datadir-rp-0", "ns-a", "redpanda-a", "pv-0")
		pvc1 := newPVC("datadir-rpb-0", "ns-b", "redpanda-b", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns-a", "datadir-rp-0", "node-a")
		pv1 := newPVWithAffinity("pv-1", "ns-b", "datadir-rpb-0", "node-b")
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.True(t, got, "chart-rendered broker pod must count toward Gate 2 via cluster.redpanda.com/broker=true")
	})

	t.Run("stuck pod whose PV node can't be resolved is skipped from counting", func(t *testing.T) {
		// pod0 pins to node-a; pod1's PV has no NodeAffinity hostname,
		// so it can't be classified. The set has only {"node-a"} → 1
		// node → not a multi-node event.
		pod0 := withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0")
		pod1 := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pvc0 := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		pvc1 := newPVC("datadir-rp-1", "ns", "redpanda", "pv-1")
		pv0 := newPVWithAffinity("pv-0", "ns", "datadir-rp-0", "node-a")
		// pv1 has no NodeAffinity at all.
		pv1 := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
			Spec: corev1.PersistentVolumeSpec{
				ClaimRef: &corev1.ObjectReference{Namespace: "ns", Name: "datadir-rp-1"},
			},
		}
		r := newController(t, s, pod0, pod1, pvc0, pvc1, pv0, pv1)
		got, err := r.multiNodeEventInProgress(ctx)
		require.NoError(t, err)
		require.False(t, got)
	})
}

// TestNodeFromPVAffinity verifies the hostname extractor used by
// Gate 2 to bucket stuck pods by their pinned node.
func TestNodeFromPVAffinity(t *testing.T) {
	cases := []struct {
		name string
		pv   *corev1.PersistentVolume
		want string
	}{
		{
			name: "kubernetes.io/hostname In selector returns hostname",
			pv:   newPVWithAffinity("pv", "ns", "claim", "node-a"),
			want: "node-a",
		},
		{
			name: "no NodeAffinity returns empty",
			pv:   &corev1.PersistentVolume{},
			want: "",
		},
		{
			name: "NodeAffinity without Required returns empty",
			pv:   &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{NodeAffinity: &corev1.VolumeNodeAffinity{}}},
			want: "",
		},
		{
			name: "non-hostname affinity key returns empty (e.g. zone topology)",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key:      "topology.kubernetes.io/zone",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"us-east-1a"},
							}},
						}},
					},
				},
			}},
			want: "",
		},
		{
			name: "hostname NotIn operator returns empty (only In is honored)",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{{
								Key:      corev1.LabelHostname,
								Operator: corev1.NodeSelectorOpNotIn,
								Values:   []string{"node-a"},
							}},
						}},
					},
				},
			}},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, nodeFromPVAffinity(tc.pv))
		})
	}
}

// TestListClusterPVCsByName verifies the PVC snapshot helper used by
// Gate 0 (cache-staleness tracker check) and Gate 3 (recreated-but-not-
// yet-bound detector). Scoping is by namespace AND
// app.kubernetes.io/instance label; pods without the instance label
// return an empty (non-nil) snapshot.
func TestListClusterPVCsByName(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)

	t.Run("no PVCs returns empty map", func(t *testing.T) {
		pod := newPod("rp-0", "ns", "redpanda")
		r := newController(t, s)
		got, err := r.listClusterPVCsByName(ctx, pod)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
	})

	t.Run("snapshot includes only same-cluster PVCs", func(t *testing.T) {
		pod := newPod("rp-0", "ns", "redpanda-a")
		want0 := newPVC("datadir-rp-0", "ns", "redpanda-a", "pv-0")
		want1 := newPVC("datadir-rp-1", "ns", "redpanda-a", "pv-1")
		other := newPVC("datadir-rpb-0", "ns", "redpanda-b", "pv-2")
		r := newController(t, s, want0, want1, other)
		got, err := r.listClusterPVCsByName(ctx, pod)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Contains(t, got, "datadir-rp-0")
		require.Contains(t, got, "datadir-rp-1")
		require.NotContains(t, got, "datadir-rpb-0")
	})

	t.Run("snapshot preserves spec.volumeName for Gate 3 inspection", func(t *testing.T) {
		pod := newPod("rp-0", "ns", "redpanda")
		bound := newPVC("datadir-rp-0", "ns", "redpanda", "pv-0")
		unbound := newPVC("datadir-rp-1", "ns", "redpanda", "")
		r := newController(t, s, bound, unbound)
		got, err := r.listClusterPVCsByName(ctx, pod)
		require.NoError(t, err)
		require.Equal(t, "pv-0", got["datadir-rp-0"].Spec.VolumeName)
		require.Equal(t, "", got["datadir-rp-1"].Spec.VolumeName)
	})

	t.Run("PVC in different namespace is excluded", func(t *testing.T) {
		pod := newPod("rp-0", "ns-a", "redpanda")
		other := newPVC("datadir-rp-0", "ns-b", "redpanda", "pv-0")
		r := newController(t, s, other)
		got, err := r.listClusterPVCsByName(ctx, pod)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("pod without instance label returns empty (non-nil) map", func(t *testing.T) {
		pod := newPod("orphan-0", "ns", "")
		other := newPVC("datadir-other-0", "ns", "redpanda", "pv-0")
		r := newController(t, s, other)
		got, err := r.listClusterPVCsByName(ctx, pod)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}
