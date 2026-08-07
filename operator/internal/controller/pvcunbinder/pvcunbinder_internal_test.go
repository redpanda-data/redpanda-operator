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
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	operatorlabels "github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
)

func newScheme(t *testing.T, withV2, withStretch, withV1 bool) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, storagev1.AddToScheme(s))
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

// newNode builds a Node whose kubernetes.io/hostname label matches
// name — the common-case default (kubelet sets it that way absent a
// --hostname-override), and what nodeUnavailableForScheduling actually
// resolves by. Tests exercising a diverging hostname label build their
// own Node object instead.
func newNode(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: map[string]string{corev1.LabelHostname: name},
	}}
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

	t.Run("freed: hostname label diverging from the Node name still holds the gate", func(t *testing.T) {
		// PV node affinity carries the kubernetes.io/hostname LABEL
		// value; under kubelet --hostname-override it differs from the
		// Node object's name. Resolving by name would report a live
		// node as gone and OPEN this gate — the fail direction that
		// re-enables cross-broker rebinding.
		pv := pvWithAnnotations("pv-0", corev1.VolumeAvailable, "worker-a", map[string]string{FreedPVAnnotation: key})
		node := newNode("ip-10-0-0-1")
		node.Labels[corev1.LabelHostname] = "worker-a"
		r := newController(t, s, pv, node)
		state, err := r.checkPVGates(ctx, key, otherPod())
		require.NoError(t, err)
		require.True(t, state.freedPVUnresolved, "the pinned node must be resolved by its hostname label, not its Node name")
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

// TestReconcileGate3StuckClaimExemption drives Reconcile end-to-end
// through Gate 3 (pvc-rebinding). Gate 3 defers while any claim in the
// cluster is unbound — but claims owned by Pods that are themselves
// stuck Pending on volume affinity (the reconciled Pod first among
// them) can never settle on their own and must be exempt, or the gate
// deadlocks the exact unbind meant to fix them.
//
// The canonical deadlock (from a production incident): a fresh
// cluster's broker never schedules because one of its two claims
// (shadow-index-cache) was provisioned onto an already-occupied node.
// With WaitForFirstConsumer binding, its OTHER claim (datadir) can
// never bind until the Pod schedules, the Pod can never schedule until
// the mis-pinned claim is unbound, and Gate 3 defers that unbind
// forever because the datadir claim has no volumeName. Three-way
// circular wait; the cluster never bootstraps. The exemption must
// hold symmetrically across multiple victims: two mis-pinned brokers
// each holding an unbound datadir claim must not defer on each
// other's.
func TestReconcileGate3StuckClaimExemption(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)

	// wffc is a WaitForFirstConsumer StorageClass named "standard".
	// PVCs meant to model the deadlock's unbound WFFC claim set
	// Spec.StorageClassName to it explicitly, matching what Kubernetes
	// itself would have already persisted onto the object at CREATE
	// time (via the DefaultStorageClass admission controller) — this
	// evaluator deliberately does NOT re-resolve "the cluster's current
	// default" for a claim whose Spec.StorageClassName is nil, so nil
	// fixtures below are exercising "no class", not "the default".
	wffc := &storagev1.StorageClass{
		ObjectMeta:        metav1.ObjectMeta{Name: "standard"},
		VolumeBindingMode: ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
	}

	// boundHostPathPV builds a PV that qualifies for unbinding:
	// Bound, ClaimRef set, NodeAffinity pinned, HostPath-backed.
	boundHostPathPV := func(name, claimName, hostname string) *corev1.PersistentVolume {
		pv := newPVWithAffinity(name, "ns", claimName, hostname)
		pv.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/data"},
		}
		pv.Status.Phase = corev1.VolumeBound
		return pv
	}

	// hardAntiAffinity builds a RequiredDuringSchedulingIgnoredDuringExecution
	// PodAntiAffinity term matching matchLabels — the shape the
	// redpanda chart renders for the default podAntiAffinity.type:
	// hard. Tests needing podAntiAffinity.type: soft/custom instead use
	// PreferredDuringScheduling (or leave Affinity nil).
	hardAntiAffinity := func(matchLabels map[string]string) *corev1.Affinity {
		return &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
					LabelSelector: &metav1.LabelSelector{MatchLabels: matchLabels},
					TopologyKey:   "kubernetes.io/hostname",
				}},
			},
		}
	}

	// softAntiAffinity builds a PreferredDuringScheduling-only
	// PodAntiAffinity — the podAntiAffinity.type: soft shape, which
	// doesn't forbid co-location and so must never count as occupancy
	// proof.
	softAntiAffinity := func(matchLabels map[string]string) *corev1.Affinity {
		return &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{MatchLabels: matchLabels},
						TopologyKey:   "kubernetes.io/hostname",
					},
				}},
			},
		}
	}

	// stuckBroker models the production victim: a Pending pod with a
	// volume-affinity scheduling failure holding an unbound WFFC
	// datadir claim and a Bound shadow-index-cache claim whose PV is
	// pinned to a node the pod can never fit on.
	stuckBroker := func(name string) (*corev1.Pod, *corev1.PersistentVolumeClaim, *corev1.PersistentVolumeClaim, *corev1.PersistentVolume) {
		pod := withPVC(withPVC(podWithVolumeAffinityFailure(name, "ns", "redpanda"), "datadir-"+name), "shadow-index-cache-"+name)
		datadir := newPVC("datadir-"+name, "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard") // already defaulted, per real admission-time behavior
		shadow := newPVC("shadow-index-cache-"+name, "ns", "redpanda", "pv-shadow-"+name)
		pv := boundHostPathPV("pv-shadow-"+name, "shadow-index-cache-"+name, "node-a")
		return pod, datadir, shadow, pv
	}

	t.Run("own unbound WFFC claim does not deadlock the unbind", func(t *testing.T) {
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		recorder := &events.FakeRecorder{Events: make(chan string, 8)}
		r := newController(t, s, wffc, pod, datadir, shadow, pv)
		r.Recorder = recorder

		exemptedBefore := promtestutil.ToFloat64(observability.PVCUnbinderGateExempted)
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "Gate 3 must not defer on the reconciled pod's own unbound claim")
		require.Equal(t, exemptedBefore+1, promtestutil.ToFloat64(observability.PVCUnbinderGateExempted), "an exemption-based gate pass must increment the exempted metric")

		// The gate override left its paper trail: an Event naming the
		// exempted claim (mirroring the deferral-event assertion
		// elsewhere in this suite).
		select {
		case ev := <-recorder.Events:
			require.Contains(t, ev, eventReasonGateExempted)
			require.Contains(t, ev, "datadir-rp-1")
		default:
			t.Fatal("expected a PVCUnbinderGateExempted event when the gate is passed via the exemption")
		}

		// The mis-pinned Bound claim was deleted...
		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the mis-pinned bound claim must be deleted")

		// ...the unbound claim was left untouched...
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-1"}, &gotPVC))

		// ...the PV was prepared for unbind (Retain + in-flight)...
		var gotPV corev1.PersistentVolume
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Name: "pv-shadow-rp-1"}, &gotPV))
		require.Equal(t, corev1.PersistentVolumeReclaimRetain, gotPV.Spec.PersistentVolumeReclaimPolicy)
		require.Equal(t, "/ns/redpanda", gotPV.Annotations[InFlightAnnotation])

		// ...and the Pod was deleted to re-trigger PVC creation.
		var gotPod corev1.Pod
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod)
		require.True(t, apierrors.IsNotFound(err), "the pod must be deleted to re-trigger PVC creation and scheduling")
	})

	t.Run("two stuck pods with unbound WFFC claims do not mutually deadlock", func(t *testing.T) {
		// The >=2-victim variant: rp-1 and rp-2 are both mis-pinned to
		// the same occupied node, each holding an unbound WFFC datadir
		// claim. Neither claim can ever settle on its own, so
		// Reconcile(rp-1) must not defer on rp-2's unbound claim (and
		// vice versa) — otherwise the deadlock survives with two
		// victims. Destructive work stays serialized by Gate 0.
		pod1, datadir1, shadow1, pv1 := stuckBroker("rp-1")
		pod2, datadir2, shadow2, pv2 := stuckBroker("rp-2")
		r := newController(t, s, wffc, pod1, datadir1, shadow1, pv1, pod2, datadir2, shadow2, pv2)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod1)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "Gate 3 must not defer on a fellow stuck pod's unbound claim")

		// rp-1 was remediated...
		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "rp-1's mis-pinned bound claim must be deleted")
		var gotPod corev1.Pod
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod)
		require.True(t, apierrors.IsNotFound(err), "rp-1 must be deleted to re-trigger PVC creation")

		// ...and rp-2 was left for its own reconcile.
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-2"}, &gotPVC))
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-2"}, &gotPod))
	})

	t.Run("own unrelated unbound claim under Immediate binding does not exempt a healthy bound claim", func(t *testing.T) {
		// rp-1 has TWO claims: "datadir" is unbound, but under an
		// Immediate-mode StorageClass — a real provisioning failure
		// (storage class, quota, capacity), NOT the WaitForFirstConsumer
		// deadlock this exemption targets — and "shadow-index-cache" is
		// a perfectly healthy Bound HostPath/Local claim that happens to
		// satisfy podHasMispinnedBoundClaim's shape test on its own.
		// Because "datadir" isn't WaitForFirstConsumer, it must not be
		// exempted, so Gate 3 keeps deferring — the healthy claim must
		// survive untouched instead of being deleted as a false-positive
		// "mis-pin".
		immediate := storagev1.VolumeBindingImmediate
		immediateSC := &storagev1.StorageClass{
			ObjectMeta:        metav1.ObjectMeta{Name: "immediate-sc"},
			VolumeBindingMode: &immediate,
		}
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("immediate-sc")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, immediateSC, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an Immediate-mode unbound claim must not be exempted; Gate 3 must keep deferring")

		// Nothing was touched — in particular the healthy bound claim
		// must survive just because it happens to be a HostPath/Local
		// claim with NodeAffinity.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("generic scheduling failure with an available pinned node does not exempt the bound claim", func(t *testing.T) {
		// rp-1 matches the weak schedulingFailureRE signature ("0/N
		// nodes are available: ... Insufficient cpu") which says
		// NOTHING about volumes — it's Pending because every node lacks
		// CPU, unrelated to storage. It happens to hold both an unbound
		// WFFC "datadir" claim AND a Bound HostPath/Local "cache" claim
		// pinned to "node-a" — the same shape a genuinely mis-pinned
		// broker has. But "node-a" here actually EXISTS, is not
		// cordoned, and no other Pod occupies it: nothing proves that
		// pinning is why rp-1 can't schedule. Without that proof, Gate 3
		// must keep deferring rather than nuking a claim that has
		// nothing to do with the CPU shortage.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a claim pinned to a provably available node must not be exempted; Gate 3 must keep deferring")

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the bound claim on an available node must not be deleted")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("multi-value hostname NodeAffinity with one available node does not exempt the bound claim", func(t *testing.T) {
		// A PV's NodeAffinity is a label selector, not a single Node
		// reference: a single `In` expression can list multiple
		// hostname values, meaning the claim can bind on ANY of them.
		// Here "node-a" is cordoned but "node-b" — also named in the
		// same expression — is healthy and available. rp-1 could still
		// bind this claim on node-b, so it's not proven mis-pinned;
		// exempting it (and deleting a claim that could still bind
		// fine) would be premature.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		healthyPV.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"node-a", "node-b"}
		cordonedA := newNode("node-a")
		cordonedA.Spec.Unschedulable = true
		availableB := newNode("node-b")
		r := newController(t, s, wffc, cordonedA, availableB, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a claim that could still bind on an available alternate node must not be exempted; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the claim must not be deleted while an alternate eligible node is available")
	})

	t.Run("multi-value hostname NodeAffinity with every eligible node unavailable is still exempted", func(t *testing.T) {
		// Same shape as above, but BOTH hostname values the PV's
		// NodeAffinity accepts are confirmed unavailable (one cordoned,
		// one simply doesn't exist) — the claim genuinely cannot bind
		// anywhere, so the exemption must still fire.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		mispinnedPV.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"node-a", "node-b"}
		cordonedA := newNode("node-a")
		cordonedA.Spec.Unschedulable = true
		// "node-b" is deliberately absent from the fixture — gone entirely.
		r := newController(t, s, wffc, cordonedA, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a claim whose every eligible node is unavailable must still be exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim must be deleted once every eligible node is confirmed unavailable")
	})

	t.Run("two Nodes sharing one hostname label fail closed even when both are cordoned", func(t *testing.T) {
		// A hostname label value resolving to MORE than one Node is a
		// misconfigured state the evaluator refuses to interpret —
		// even when every candidate looks unavailable, it
		// conservatively reports the node available and withholds the
		// exemption, so Gate 3 keeps deferring.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		cordonedA := newNode("node-a")
		cordonedA.Spec.Unschedulable = true
		doppelganger := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   "node-a-doppelganger",
			Labels: map[string]string{corev1.LabelHostname: "node-a"},
		}}
		doppelganger.Spec.Unschedulable = true
		r := newController(t, s, wffc, cordonedA, doppelganger, pod, datadir, shadow, pv)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an ambiguous hostname resolution must fail closed; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the bound claim must not be deleted on ambiguous node resolution")
	})

	t.Run("hostname label diverging from the Node's metadata.name still resolves correctly", func(t *testing.T) {
		// A PV's NodeAffinity matches against the kubernetes.io/hostname
		// LABEL, not the Node object's own metadata.name — they're
		// independent fields that merely default to the same value.
		// Here the Node actually named "node-a" is registered under a
		// DIFFERENT object name ("actual-node-object-name") with only
		// its hostname LABEL set to "node-a", and it's healthy
		// (uncordoned, Ready, unoccupied). Resolving by Get(name=
		// "node-a") — the pre-fix behavior — would find nothing and
		// wrongly conclude the node is gone/unavailable, exempting and
		// deleting a perfectly healthy claim. Resolving by the label
		// (the fix) finds the real, available Node and correctly
		// declines to exempt.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		divergentNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "actual-node-object-name",
				Labels: map[string]string{corev1.LabelHostname: "node-a"},
			},
		}
		r := newController(t, s, wffc, divergentNode, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "resolving by the hostname label must find the real, available Node; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy claim must not be deleted just because no Node is NAMED node-a")
	})

	t.Run("Insufficient-cpu message with a cordoned pinned node is still exempted (intentional, not a false positive)", func(t *testing.T) {
		// This is the scenario an adversarial review flagged as a
		// possible false positive: rp-1's Pending message blames "3
		// Insufficient cpu" — nothing about storage or volume affinity
		// — while its mis-pinned "shadow-index-cache" claim happens to
		// sit on a cordoned node. That combination is still correctly
		// exempted, and deleting the mis-pinned claim is still
		// correct, because the message was never what's being trusted
		// here: a Bound HostPath/Local claim's NodeAffinity confines
		// rp-1 to exactly "node-a" (Kubernetes will not schedule a Pod
		// anywhere its Bound volume isn't pinned to), and "node-a"
		// being cordoned is independent, mechanical proof rp-1 cannot
		// schedule there — full stop, regardless of what the
		// aggregate message additionally blames on other nodes. That
		// holds even if a cluster-wide CPU shortage also exists;
		// freeing the mis-pinned claim can only help or be neutral,
		// never actively harmful, and any residual CPU shortage is
		// outside this controller's scope either way. See the doc
		// comments on exemptClaimNames and nodeUnavailableForScheduling
		// for the full reasoning. Requiring the message to explicitly
		// name volume/node affinity instead would reintroduce the
		// original production bug this whole exemption exists to fix
		// (see stuckClaimNames's doc comment) — Kubernetes doesn't
		// reliably surface that attribution.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		cordoned := newNode("node-a")
		cordoned.Spec.Unschedulable = true
		r := newController(t, s, wffc, cordoned, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a claim pinned to a cordoned node must still be exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim pinned to the cordoned node must be deleted")
	})

	t.Run("generic scheduling failure with a hard-anti-affinity-matching occupant on the pinned node is still exempted", func(t *testing.T) {
		// The canonical production shape: rp-1 has a required
		// (hard) PodAntiAffinity term matching rp-0's labels, and rp-0
		// already occupies "node-a" (Spec.NodeName) — the exact node
		// rp-1's cache PV is pinned to. That's the concrete evidence
		// the generic message alone can't provide — the exemption must
		// still fire.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = hardAntiAffinity(map[string]string{operatorlabels.InstanceKey: "redpanda"})
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("rp-0", "ns", "redpanda") // carries operatorlabels.InstanceKey: "redpanda", matching pod's required term
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, mispinned, mispinnedPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a claim pinned to a node occupied by a hard-anti-affinity match must still be exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim pinned to the occupied node must be deleted")
	})

	t.Run("occupant outside the instance label set still counts when the pod's own term selects it", func(t *testing.T) {
		// A custom hard anti-affinity term may deliberately select a
		// DIFFERENT workload sharing the namespace. The scheduler
		// rejects the node for any matching occupant, so the occupant
		// scan must consider every pod in the namespace — an
		// instance-scoped list would hide this occupant and withhold
		// the proof leg the term's shape qualifies for.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = hardAntiAffinity(map[string]string{"app": "other-heavy-workload"})
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "heavy-0",
				Namespace: "ns",
				Labels:    map[string]string{"app": "other-heavy-workload"},
			},
			Spec:   corev1.PodSpec{NodeName: "node-a"},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, mispinned, mispinnedPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "an anti-affinity-matching occupant from another workload must still prove the node unavailable")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim pinned to the occupied node must be deleted")
	})

	t.Run("hard-anti-affinity term that does not match the occupant's labels does not exempt the bound claim", func(t *testing.T) {
		// rp-1 DOES have a required PodAntiAffinity term, but it
		// selects on a label the occupant doesn't carry (e.g. a
		// component-scoped selector that only matches rp-1's own
		// pool). An occupant on the pinned node that fails to match
		// the term proves nothing, regardless of sharing the instance
		// label — Gate 3 must keep deferring.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = hardAntiAffinity(map[string]string{"app.kubernetes.io/component": "redpanda-pool-a-statefulset"})
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("other-pool-0", "ns", "redpanda") // no "app.kubernetes.io/component" label at all
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a non-matching occupant must not be treated as proof of unavailability; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	// assertUnsupportedTermShapeDefers builds rp-1 with a single
	// RequiredDuringSchedulingIgnoredDuringExecution term (using
	// matchLabels that WOULD match rp-0) and an rp-0 occupant sitting
	// on rp-1's pinned node, then asserts Gate 3 still defers: real
	// Kubernetes PodAffinityTerm semantics are richer than a bare
	// LabelSelector match (Namespaces/NamespaceSelector,
	// TopologyKey-scoped node-label comparison,
	// matchLabelKeys/mismatchLabelKeys), and podRequiredAntiAffinityMatches
	// deliberately doesn't implement all of it — so a term outside the
	// one shape it does interpret must not be trusted as proof, even
	// though a naive selector-only check (the very bug this guards
	// against) would have matched.
	assertUnsupportedTermShapeDefers := func(t *testing.T, term corev1.PodAffinityTerm) {
		t.Helper()
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{term},
			},
		}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("rp-0", "ns", "redpanda") // carries operatorlabels.InstanceKey: "redpanda"
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an unsupported term shape must not be treated as proof of unavailability; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	}

	t.Run("required term naming a different namespace does not exempt the bound claim", func(t *testing.T) {
		// Namespaces naming a namespace OTHER than rp-1's own ("ns")
		// changes the term's meaning away from "this pod's own
		// namespace" — this evaluator only ever looks at pods in pod's
		// own namespace, so it can't correctly decide whether such a
		// term is satisfied and skips it rather than misread it.
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:   "kubernetes.io/hostname",
			Namespaces:    []string{"other-ns"},
		})
	})

	t.Run("required term naming multiple namespaces (including rp-1's own) does not exempt the bound claim", func(t *testing.T) {
		// A multi-element Namespaces list — even one that happens to
		// include rp-1's own namespace — applies to the UNION of those
		// namespaces, which this evaluator can't fully verify (it only
		// scans rp-1's own namespace), so it's skipped rather than
		// misread as "own namespace only".
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:   "kubernetes.io/hostname",
			Namespaces:    []string{"ns", "other-ns"},
		})
	})

	t.Run("required term with Namespaces set to exactly the pod's own namespace is still exempted", func(t *testing.T) {
		// Per the v1 Cluster's default hard anti-affinity
		// (operator/pkg/resources.StatefulSetResource.obj), Namespaces
		// is always set explicitly to []string{pod.Namespace} rather
		// than left unset. Per the API's own doc, that's semantically
		// IDENTICAL to leaving Namespaces unset — both mean "this
		// pod's own namespace" — so it must still count as proof, not
		// be skipped just because the field happens to be non-empty.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
					LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
					Namespaces:    []string{"ns"}, // rp-1's own namespace, named explicitly
					TopologyKey:   "kubernetes.io/hostname",
				}},
			},
		}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("rp-0", "ns", "redpanda")
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, mispinned, mispinnedPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "an explicit Namespaces naming only the pod's own namespace must still be treated as proof of unavailability")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim pinned to the occupied node must be deleted")
	})

	t.Run("required term with a NamespaceSelector does not exempt the bound claim", func(t *testing.T) {
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:       "kubernetes.io/hostname",
			NamespaceSelector: &metav1.LabelSelector{}, // matches all namespaces per the API's own semantics
		})
	})

	t.Run("required term with a non-hostname TopologyKey does not exempt the bound claim", func(t *testing.T) {
		// A zone/region (or any other custom) TopologyKey would
		// require comparing the pinned Node's own label value against
		// occupant's Node's label value — this evaluator doesn't
		// resolve Node labels for that, so it can't safely assume
		// same-Node implies same-domain for a key other than hostname.
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:   "topology.kubernetes.io/zone",
		})
	})

	t.Run("required term with a missing TopologyKey does not exempt the bound claim", func(t *testing.T) {
		// Empty TopologyKey is invalid on a real API object, but
		// defensively handled the same way as any other unsupported key.
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:   "",
		})
	})

	t.Run("required term with MatchLabelKeys does not exempt the bound claim", func(t *testing.T) {
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:    "kubernetes.io/hostname",
			MatchLabelKeys: []string{"apps.kubernetes.io/pod-index"},
		})
	})

	t.Run("required term with MismatchLabelKeys does not exempt the bound claim", func(t *testing.T) {
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{operatorlabels.InstanceKey: "redpanda"}},
			TopologyKey:       "kubernetes.io/hostname",
			MismatchLabelKeys: []string{"apps.kubernetes.io/pod-index"},
		})
	})

	t.Run("required term with an invalid LabelSelector does not exempt the bound claim", func(t *testing.T) {
		// A term whose selector fails LabelSelectorAsSelector is
		// skipped rather than treated as an error or a wildcard —
		// malformed affinity is the admission layer's problem to
		// reject, not a reason to guess at occupancy proof.
		assertUnsupportedTermShapeDefers(t, corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "app",
				Operator: "Bogus",
				Values:   []string{"x"},
			}}},
			TopologyKey: "kubernetes.io/hostname",
		})
	})

	t.Run("soft-only podAntiAffinity with a matching same-instance occupant does not exempt the bound claim", func(t *testing.T) {
		// Exactly the regression this exemption must not misfire on:
		// the redpanda chart's podAntiAffinity.type can be "soft"
		// (PreferredDuringScheduling — doesn't forbid co-location) or
		// "custom", and a Pod's template can override affinity
		// entirely. Here rp-1 only has a PREFERRED anti-affinity term
		// (or, in the "absent" variant, none at all); rp-0 — otherwise
		// a perfectly matching occupant on the same node — proves
		// nothing under soft/absent affinity, so a generic CPU failure
		// must not be escalated into deleting rp-1's healthy bound
		// claim.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = softAntiAffinity(map[string]string{operatorlabels.InstanceKey: "redpanda"})
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("rp-0", "ns", "redpanda") // would match pod's selector if the term were required
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "soft anti-affinity must not be treated as proof of unavailability; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("absent Affinity with a matching same-instance occupant does not exempt the bound claim", func(t *testing.T) {
		// The "absent" counterpart to the soft-only case above: rp-1
		// has no Affinity at all (e.g. podAntiAffinity.type: custom
		// with an empty custom block, or a hand-edited pod template
		// with no anti-affinity). rp-0 sits on the pinned node and
		// would match a same-instance selector, but with nothing to
		// prove a scheduling conflict, Gate 3 must keep deferring.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		occupant := newPod("rp-0", "ns", "redpanda")
		occupant.Spec.NodeName = "node-a"
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV, occupant)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an occupant must not be treated as proof of unavailability without any required anti-affinity; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("Node lookup uses the uncached Reader, not the cached Client", func(t *testing.T) {
		// nodeUnavailableForScheduling must read the candidate PV's
		// pinned Node through the uncached Reader (matching
		// freedPVBlocking): this controller's RBAC grants Node get and
		// list — not watch — so a cache-backed Get here would force
		// (and fail without) a cluster-wide Node watch on a real,
		// RBAC-restricted install. The cached Client
		// below has NO Node object at all, standing in for that
		// permission gap; a healthy, existing, uncordoned "node-a" is
		// only reachable through Reader. If the code ever regresses to
		// reading Nodes off Client, it would see NotFound and wrongly
		// treat "node-a" as unavailable, exempting a claim that (per
		// the real Node state, visible only via Reader) isn't actually
		// mis-pinned.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, pod, datadir, healthy, healthyPV) // no Node in the cached Client
		// The Reader (standing in for fresh API-server state) carries
		// the stuck Pod, the PVC evidence, and the healthy Node; the
		// Node is the only object whose visibility differs between the
		// two clients.
		r.Reader = fake.NewClientBuilder().WithScheme(s).WithObjects(pod, newNode("node-a"), datadir, healthy).Build()

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the healthy Node seen via Reader must not be exempted; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("occupant Pod lookup uses the uncached Reader, not a stale cached Client", func(t *testing.T) {
		// The occupancy check must read sibling Pods through the
		// uncached Reader, exactly like the Node lookup above: if
		// occupant rp-0 was ALREADY deleted or rescheduled elsewhere by
		// the time this Reconcile runs, but the informer cache hasn't
		// caught up yet, trusting the stale cached copy would
		// manufacture an anti-affinity conflict that no longer exists —
		// on a Node that may by now be perfectly schedulable — and
		// destructively delete rp-1's healthy bound claim for nothing.
		// Here the cached Client still has rp-0 sitting on "node-a"
		// (stale, no DeletionTimestamp, old Spec.NodeName); the
		// uncached Reader — standing in for current API-server state —
		// has no such Pod at all.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Affinity = hardAntiAffinity(map[string]string{operatorlabels.InstanceKey: "redpanda"})
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		staleOccupant := newPod("rp-0", "ns", "redpanda")
		staleOccupant.Spec.NodeName = "node-a"
		// The cached Client still has the stale occupant on "node-a"...
		r := newController(t, s, wffc, newNode("node-a"), pod, datadir, healthy, healthyPV, staleOccupant)
		// ...but the uncached Reader (fresher API-server state) does
		// not — it carries the stuck Pod, the PVC evidence, and the
		// healthy Node, just no occupant.
		r.Reader = fake.NewClientBuilder().WithScheme(s).WithObjects(pod, newNode("node-a"), datadir, healthy).Build()

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a stale cached occupant must not be treated as proof of unavailability; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("sibling discovery uses the uncached Reader, not a stale cached Client", func(t *testing.T) {
		// The sibling scan feeds ShouldRemediate's Timeout freshness
		// check, which is only as fresh as the read behind it: sibling
		// rp-0 was genuinely stuck, then got resolved out-of-band (its
		// recreated Pod scheduled onto node-b and is Running), leaving
		// its datadir claim genuinely mid-settling — exactly what Gate 3
		// must now defer on. The cached Client still serves the OLD
		// stuck rp-0 (Pending, volume-affinity failure, mis-pinned bound
		// claim — evidence that would fully re-manufacture the
		// exemption); the uncached Reader — standing in for current
		// API-server state — serves the fresh Running rp-0, which fails
		// pvcUnbinderPredicate and earns no exemption. Gate 3 must
		// defer on rp-0's unbound claim instead of letting rp-1's unbind
		// proceed concurrently with rp-0's in-flight recovery.
		pod := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		mispinned := newPVC("datadir-rp-1", "ns", "redpanda", "pv-data-1")
		pv := boundHostPathPV("pv-data-1", "datadir-rp-1", "node-a")

		staleSibling := withPVC(withPVC(podWithVolumeAffinityFailure("rp-0", "ns", "redpanda"), "datadir-rp-0"), "shadow-index-cache-rp-0")
		freshSibling := newPod("rp-0", "ns", "redpanda")
		freshSibling.Status.Phase = corev1.PodRunning
		freshSibling.Spec.NodeName = "node-b"

		siblingDatadir := newPVC("datadir-rp-0", "ns", "redpanda", "")
		siblingDatadir.Spec.StorageClassName = ptr.To("standard")
		siblingShadow := newPVC("shadow-index-cache-rp-0", "ns", "redpanda", "pv-shadow-rp-0")
		siblingShadowPV := boundHostPathPV("pv-shadow-rp-0", "shadow-index-cache-rp-0", "node-a")

		cordoned := newNode("node-a")
		cordoned.Spec.Unschedulable = true

		// The cached Client still holds the stale stuck rp-0 with its
		// full (stale-consistent) exemption evidence...
		r := newController(t, s, wffc, pod, mispinned, pv, staleSibling, siblingDatadir, siblingShadow, siblingShadowPV)
		// ...while the Reader holds the fresh Running rp-0 plus the
		// live PVC/Node evidence.
		r.Reader = fake.NewClientBuilder().WithScheme(s).WithObjects(
			pod, freshSibling, cordoned, mispinned, siblingDatadir, siblingShadow,
		).Build()

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a stale cached sibling must not re-manufacture the exemption; Gate 3 must defer on the fresh state")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-1"}, &gotPVC), "rp-1's bound claim must not be deleted")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("generic scheduling failure with a NotReady/unreachable pinned node is still exempted", func(t *testing.T) {
		// A real, common node-loss shape distinct from cordoning: the
		// Node object still exists but its kubelet crashed or it's
		// network-partitioned, so Ready is False/Unknown. Without
		// recognizing this, a stuck pod with an unbound WFFC sibling
		// claim would defer at Gate 3 forever instead of unbinding the
		// pinned claim — exactly the scenario ShouldRemediate already
		// accepts via the "untolerated taint
		// {node.kubernetes.io/unreachable: }" scheduler message.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 node(s) had untolerated taint {node.kubernetes.io/unreachable: }.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		unreachable := newNode("node-a")
		unreachable.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionUnknown}}
		r := newController(t, s, wffc, unreachable, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a claim pinned to a NotReady/unreachable node must still be exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the claim pinned to the unreachable node must be deleted")
	})

	t.Run("generic scheduling failure with the standard unreachable taint (no Ready condition set) is still exempted", func(t *testing.T) {
		// Covers the taint check independently of the Ready-condition
		// check (they're normally applied together by the node
		// lifecycle controller, but this guards against the lag window
		// between the two, and against relying on Ready alone).
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 node(s) had untolerated taint {node.kubernetes.io/unreachable: }.",
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		tainted := newNode("node-a")
		tainted.Spec.Taints = []corev1.Taint{{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoExecute}}
		r := newController(t, s, wffc, tainted, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a claim pinned to a node carrying the unreachable taint must still be exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err))
	})

	t.Run("pod's default grace-period toleration for the unreachable taint does not make the node look available", func(t *testing.T) {
		// Every Pod gets an auto-injected toleration for
		// node.kubernetes.io/unreachable with a finite
		// TolerationSeconds (the DefaultTolerationSeconds admission
		// plugin's eviction grace period) unless it declares its own.
		// That grace period is about eviction timing, not about the
		// node being fine to (re)schedule onto, and must not cause the
		// unreachable node to be treated as tolerated/available.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 node(s) had untolerated taint {node.kubernetes.io/unreachable: }.",
		}}
		pod.Spec.Tolerations = []corev1.Toleration{{
			Key:               taintNodeUnreachable,
			Operator:          corev1.TolerationOpExists,
			Effect:            corev1.TaintEffectNoExecute,
			TolerationSeconds: ptr.To(int64(300)),
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard")
		mispinned := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-mispinned-rp-1")
		mispinnedPV := boundHostPathPV("pv-mispinned-rp-1", "shadow-index-cache-rp-1", "node-a")
		tainted := newNode("node-a")
		tainted.Spec.Taints = []corev1.Taint{{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoExecute}}
		r := newController(t, s, wffc, tainted, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "the default grace-period toleration must not make the unreachable node look available")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err))
	})

	t.Run("pod's unconditional toleration for the unreachable taint leaves it unproven, so Gate 3 keeps deferring", func(t *testing.T) {
		// The flip side: with an unconditional (no TolerationSeconds)
		// toleration for the taint, and no Ready condition set, the
		// taint alone can no longer prove the node is unavailable to
		// this Pod — and nothing else does either (node exists,
		// uncordoned, unoccupied), so the exemption must not fire.
		pod := withPVC(withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		pod.Spec.Tolerations = []corev1.Toleration{{
			Key:      taintNodeUnreachable,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoExecute,
		}}
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		// WFFC class set so the ONLY thing standing between deferral
		// and exemption is the evaluator under test — without it,
		// claimUsesWaitForFirstConsumer fails the claim unconditionally
		// and the test would pass even with the evaluator broken.
		datadir.Spec.StorageClassName = ptr.To("standard")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		tainted := newNode("node-a")
		// Both effect twins, as on a real unreachable node — the
		// NoSchedule twin must be judged through the same NoExecute
		// toleration lens, not its raw effect.
		tainted.Spec.Taints = []corev1.Taint{
			{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoExecute},
			{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoSchedule},
		}
		r := newController(t, s, wffc, tainted, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an unconditionally-tolerated taint must not by itself prove the node unavailable")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("tolerate-forever mode keeps a transiently NotReady node unproven despite the Ready condition", func(t *testing.T) {
		// Under --broker-pod-node-unavailable-toleration=-1s the
		// operator injects unconditional (no TolerationSeconds)
		// not-ready/unreachable NoExecute tolerations onto broker
		// pods, and that mode's documented contract is that only
		// Node-object DELETION signals permanent node loss. For such
		// pods the exemption must be withheld as a POLICY choice (the
		// scheduler might refuse this node right now because of the
		// NoSchedule twin taint, but the flag says transient
		// unreachability is never permanent loss), so Gate 3 must keep
		// deferring instead of escalating the transient partition into
		// deleting the pod's bound claim.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		pod.Spec.Tolerations = []corev1.Toleration{
			{Key: taintNodeNotReady, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
			{Key: taintNodeUnreachable, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute},
		}
		unreachable := newNode("node-a")
		unreachable.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionUnknown}}
		// A real unreachable node carries BOTH effect twins: the node
		// lifecycle controller's eviction pass applies NoExecute and
		// its condition pass applies NoSchedule, keyed off the same
		// Ready condition. The -1s-injected tolerations are
		// NoExecute-shaped only, so the NoSchedule twin is exactly the
		// shape that must NOT defeat the carve-out.
		unreachable.Spec.Taints = []corev1.Taint{
			{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoExecute},
			{Key: taintNodeUnreachable, Effect: corev1.TaintEffectNoSchedule},
		}
		r := newController(t, s, wffc, unreachable, pod, datadir, shadow, pv)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a tolerate-forever pod's NotReady node must not be treated as unavailable; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the bound claim must survive a transiently unreachable node in tolerate-forever mode")
	})

	t.Run("grace-period tolerations do not suppress the Ready-condition proof", func(t *testing.T) {
		// The counterpart: the DEFAULT auto-injected tolerations carry
		// a finite TolerationSeconds (an eviction grace window, not a
		// statement that the node is fine), so with those the
		// Ready-condition leg must still prove unavailability and the
		// exemption must still fire.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		pod.Spec.Tolerations = []corev1.Toleration{
			{Key: taintNodeNotReady, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To(int64(300))},
			{Key: taintNodeUnreachable, Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To(int64(300))},
		}
		unreachable := newNode("node-a")
		unreachable.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionUnknown}}
		r := newController(t, s, wffc, unreachable, pod, datadir, shadow, pv)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "grace-period tolerations must not suppress the Ready-condition unavailability proof")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the mis-pinned bound claim must be deleted")
	})

	t.Run("evidence-read failure downgrades to the conservative deferral instead of failing the reconcile", func(t *testing.T) {
		// RBAC version skew (operator image upgraded ahead of its
		// ClusterRole) surfaces as a 403 on the exemption chain's
		// nodes LIST. That must not error-loop the reconcile — which
		// would fire neither the gate metric nor the deferral Event —
		// but downgrade to the pre-exemption behavior: Gate 3 defers
		// on the unbound claim with the usual 30s requeue.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		c := fake.NewClientBuilder().WithScheme(s).
			WithObjects(wffc, pod, datadir, shadow, pv).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.NodeList); ok {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "", fmt.Errorf("RBAC version skew"))
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()
		r := &Controller{Client: c}

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err, "an evidence-read failure must not fail the reconcile")
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "Gate 3 must fall back to the conservative deferral")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "nothing may be deleted when the evidence chain is unavailable")
	})

	t.Run("evidence reads are skipped entirely when no claim is unbound", func(t *testing.T) {
		// The unbinder's original scenario — node dead, ALL claims
		// Bound — must not depend on the exemption evidence chain at
		// all: with zero unbound claims Gate 3 has nothing to defer
		// on, so the (broken, per the interceptor) nodes LIST must
		// never run and remediation must proceed exactly as it did
		// before the exemption existed.
		pod := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		mispinned := newPVC("datadir-rp-1", "ns", "redpanda", "pv-data-1")
		pv := boundHostPathPV("pv-data-1", "datadir-rp-1", "node-a")
		c := fake.NewClientBuilder().WithScheme(s).
			WithObjects(wffc, pod, mispinned, pv).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.NodeList); ok {
						return apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, "", fmt.Errorf("RBAC version skew"))
					}
					return cl.List(ctx, list, opts...)
				},
			}).Build()
		r := &Controller{Client: c}

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err, "the all-claims-bound path must not touch the evidence chain")
		require.Zero(t, res.RequeueAfter)

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the mis-pinned bound claim must be deleted despite the broken nodes LIST")
	})

	t.Run("reconciled Pod is re-qualified on the uncached Reader before evidence or destruction", func(t *testing.T) {
		// The initial cached Get is only a pre-filter: if the informer
		// still serves an OLD rp-1 (stuck past the Timeout, mis-pinned
		// claims and all) while the API server already has rp-1
		// resolved (here: Running on node-b after a recreate), the
		// stale copy must not supply the exemption evidence and reach
		// the PVC deletes — the claim preconditions guard the claims,
		// not the Pod evidence that justified deleting them. The
		// cached Client below holds the fully-armed stale rp-1; the
		// uncached Reader holds the fresh Running rp-1 plus everything
		// the stale path would need to succeed, so a regression to
		// cached-only qualification destroys the claim and fails this
		// test.
		stalePod, datadir, shadow, pv := stuckBroker("rp-1")
		freshPod := newPod("rp-1", "ns", "redpanda")
		freshPod.Status.Phase = corev1.PodRunning
		freshPod.Spec.NodeName = "node-b"
		cordoned := newNode("node-a")
		cordoned.Spec.Unschedulable = true
		r := newController(t, s, wffc, stalePod, datadir, shadow, pv)
		r.Reader = fake.NewClientBuilder().WithScheme(s).WithObjects(
			freshPod, datadir, shadow, cordoned, wffc,
		).Build()

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(stalePod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a Pod that no longer qualifies on fresh state is simply skipped")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "no claim may be deleted on stale Pod evidence")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod), "the Pod must not be deleted on stale evidence")
	})

	t.Run("a sibling's deadlock proof does not authorize destroying a Pod without its own mis-pin proof", func(t *testing.T) {
		// Exemptions exist to break deadlocks, not to transitively
		// unlock unrelated destruction: rp-0 is genuinely deadlocked
		// (its shadow PV pinned to gone node-a, unbound WFFC datadir),
		// while the reconciled rp-1 is Pending on plain CPU pressure
		// with a healthy bound claim whose multi-value NodeAffinity
		// [node-a, node-b] still has available node-b — rp-1's own
		// mis-pin proof fails. rp-0's exemption clears the only
		// unbound claim, which pre-exemption would have incidentally
		// deferred rp-1's destruction; the post-exemption own-proof
		// check must restore that deferral instead of deleting rp-1's
		// healthy claim. (Both PVs pin node-a as Values[0], so Gate
		// 2's distinct-node count stays at one and does not mask the
		// property under test.)
		sibling, siblingDatadir, siblingShadow, siblingPV := stuckBroker("rp-0")
		pod := withPVC(newPod("rp-1", "ns", "redpanda"), "datadir-rp-1")
		pod.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient cpu.",
		}}
		healthy := newPVC("datadir-rp-1", "ns", "redpanda", "pv-data-1")
		healthyPV := boundHostPathPV("pv-data-1", "datadir-rp-1", "node-a")
		healthyPV.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"node-a", "node-b"}
		// node-a is gone entirely (rp-0's proof); node-b is present
		// and healthy (defeats rp-1's own proof).
		r := newController(t, s, wffc, newNode("node-b"), pod, healthy, healthyPV, sibling, siblingDatadir, siblingShadow, siblingPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the sibling's exemption must not authorize destroying rp-1 without rp-1's own mis-pin proof")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-1"}, &gotPVC), "rp-1's healthy bound claim must not be deleted")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("own unbound claim with explicit empty storageClassName does not exempt a healthy bound claim", func(t *testing.T) {
		// storageClassName: "" is a distinct, explicit Kubernetes shape
		// ("no storage class, no dynamic provisioning" — what the
		// redpanda chart produces via `storage.persistentVolume.storageClass: "-"`).
		// With no beta annotation present (the annotation wins whenever
		// its key exists; Spec.StorageClassName applies only when it is
		// absent, as here), the explicit "" must resolve to "no class"
		// — even though a WaitForFirstConsumer StorageClass exists
		// elsewhere in the cluster, it must NOT be consulted for this
		// claim. "shadow-index-cache" is a healthy Bound HostPath/Local
		// claim that must survive.
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("")
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "an explicit empty storageClassName must resolve to no class and not be exempted; Gate 3 must keep deferring")

		// Nothing was touched — in particular the healthy bound claim
		// must survive; it must not be deleted just because an
		// unrelated WaitForFirstConsumer StorageClass happens to exist
		// in the cluster.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("nil StorageClassName does not fall back to the cluster's current WFFC-mode StorageClass", func(t *testing.T) {
		// This is the finding an adversarial review flagged: a claim
		// left with Spec.StorageClassName == nil and no beta annotation
		// must resolve to "no class", NOT to whatever StorageClass
		// currently happens to be present/marked default in the
		// cluster. Kubernetes' own DefaultStorageClass admission
		// controller mutates Spec.StorageClassName onto the PERSISTED
		// object at CREATE time — the one and only time defaulting
		// happens — so a still-nil field on an existing claim means
		// admission found no default to apply back then (or was
		// disabled), not "consult today's default instead". "wffc" here
		// stands in for a StorageClass that exists in the cluster but
		// must NOT be treated as governing this claim.
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "") // Spec.StorageClassName left nil, no annotation
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a nil StorageClassName must not fall back to a StorageClass that merely exists in the cluster; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("beta storage-class annotation pointing at a non-WFFC class does not exempt despite a WFFC class existing", func(t *testing.T) {
		// A legacy PVC (or one created by a client that still writes
		// the pre-1.6 annotation instead of Spec.StorageClassName) is
		// governed by that annotation, exactly as the PV controller's
		// own GetPersistentVolumeClaimClass resolves it. Here the
		// annotation names an Immediate-mode class — a real
		// provisioning failure, not the WaitForFirstConsumer deadlock
		// this exemption targets — even though "wffc" (unreferenced)
		// also exists in the cluster.
		immediate := storagev1.VolumeBindingImmediate
		immediateSC := &storagev1.StorageClass{
			ObjectMeta:        metav1.ObjectMeta{Name: "immediate-sc"},
			VolumeBindingMode: &immediate,
		}
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Annotations = map[string]string{corev1.BetaStorageClassAnnotation: "immediate-sc"}
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, immediateSC, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the beta-annotated Immediate-mode class must govern, not the unrelated WFFC class; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("beta storage-class annotation pointing at a WFFC class is honored and still exempted", func(t *testing.T) {
		// The positive counterpart: with no Spec.StorageClassName set,
		// the beta annotation naming a WaitForFirstConsumer class must
		// actually be resolved (not silently ignored) for the
		// exemption to be granted where Kubernetes itself would
		// consider the claim WaitForFirstConsumer.
		pod, datadir, mispinned, mispinnedPV := stuckBroker("rp-1")
		datadir.Spec.StorageClassName = nil
		datadir.Annotations = map[string]string{corev1.BetaStorageClassAnnotation: "standard"}
		r := newController(t, s, wffc, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a beta-annotated WaitForFirstConsumer class must be honored and exempted")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the mis-pinned bound claim must be deleted")
	})

	t.Run("conflicting spec and annotation: annotation names Immediate, Spec names WFFC — annotation governs, not exempted", func(t *testing.T) {
		// Kubernetes' own component-helpers/storage/volume.GetPersistentVolumeClaimClass
		// (vendored at this module's pinned v0.35.1) checks the beta
		// annotation FIRST — whenever the key is present at all — and
		// only consults Spec.StorageClassName when the annotation key
		// is entirely absent. That's the reverse of what might seem
		// intuitive (the modern Spec field "should" win), but a PVC
		// carrying both must resolve via the annotation to match how
		// the PV controller actually binds it. Here Spec names a WFFC
		// class but the annotation names an Immediate one — the
		// annotation must govern, so this must NOT be exempted.
		immediate := storagev1.VolumeBindingImmediate
		immediateSC := &storagev1.StorageClass{
			ObjectMeta:        metav1.ObjectMeta{Name: "immediate-sc"},
			VolumeBindingMode: &immediate,
		}
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard") // WFFC
		datadir.Annotations = map[string]string{corev1.BetaStorageClassAnnotation: "immediate-sc"}
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, immediateSC, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the annotation's Immediate-mode class must govern over a conflicting WFFC Spec.StorageClassName; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("conflicting spec and annotation: annotation names WFFC, Spec names Immediate — annotation governs, still exempted", func(t *testing.T) {
		// The mirror image of the case above: the annotation names the
		// WFFC class while Spec names an unrelated Immediate-mode
		// class. The annotation still governs, so this must be exempted.
		immediate := storagev1.VolumeBindingImmediate
		immediateSC := &storagev1.StorageClass{
			ObjectMeta:        metav1.ObjectMeta{Name: "immediate-sc"},
			VolumeBindingMode: &immediate,
		}
		pod, datadir, mispinned, mispinnedPV := stuckBroker("rp-1")
		datadir.Spec.StorageClassName = ptr.To("immediate-sc")
		datadir.Annotations = map[string]string{corev1.BetaStorageClassAnnotation: "standard"}
		r := newController(t, s, wffc, immediateSC, pod, datadir, mispinned, mispinnedPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "the annotation's WFFC class must govern over a conflicting Immediate-mode Spec.StorageClassName")

		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err), "the mis-pinned bound claim must be deleted")
	})

	t.Run("present-but-empty annotation overrides a WFFC Spec.StorageClassName", func(t *testing.T) {
		// A present annotation with an empty string value still counts
		// as "present" for Kubernetes' own precedence — it resolves to
		// "no storage class", and still wins over Spec.StorageClassName
		// even when the Spec names a real WFFC class. Getting this
		// wrong (treating a present-but-empty annotation as "absent,
		// fall back to Spec") would wrongly exempt this claim.
		pod := withPVC(withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1"), "shadow-index-cache-rp-1")
		datadir := newPVC("datadir-rp-1", "ns", "redpanda", "")
		datadir.Spec.StorageClassName = ptr.To("standard") // WFFC
		datadir.Annotations = map[string]string{corev1.BetaStorageClassAnnotation: ""}
		healthy := newPVC("shadow-index-cache-rp-1", "ns", "redpanda", "pv-healthy-rp-1")
		healthyPV := boundHostPathPV("pv-healthy-rp-1", "shadow-index-cache-rp-1", "node-a")
		r := newController(t, s, wffc, pod, datadir, healthy, healthyPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a present-but-empty annotation must resolve to no class and override Spec.StorageClassName; Gate 3 must keep deferring")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "the healthy bound claim must not be deleted")
	})

	t.Run("allow-pv-rebinding keeps the conservative deferral even for own claims", func(t *testing.T) {
		// Under --allow-pv-rebinding the unbinder floats freed PVs as
		// live binding candidates (see FreedPVAnnotation); acting while
		// ANY claim is unbound risks pairing that claim with a freed
		// disk it was never meant to hold (the INC-2818 cross-claim
		// swap, intra-pod variant). The exemption must not apply.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		r := newController(t, s, wffc, pod, datadir, shadow, pv)
		r.AllowRebinding = true

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "rebinding mode must keep deferring on any unbound claim")

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("DisableStuckClaimExemption kill switch restores the conservative deferral", func(t *testing.T) {
		// The escape hatch for environments where the proof chain
		// misfires: with the flag set, the exemption never runs and
		// Gate 3 defers on every unbound claim (the pre-exemption
		// behavior), while the rest of the unbinder keeps working.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		r := newController(t, s, wffc, pod, datadir, shadow, pv)
		r.DisableStuckClaimExemption = true

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the kill switch must restore deferral on every unbound claim")

		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC), "nothing may be deleted with the exemption disabled")
	})

	t.Run("mis-pin proof requires the PV to reference the claim back", func(t *testing.T) {
		// spec.volumeName is user-settable at claim creation (static
		// pre-binding), so a claim pointing at a PV is not evidence of
		// a binding. Only the binder completes the two-way link by
		// stamping the PV's ClaimRef with the claim's UID. A PV whose
		// ClaimRef is missing or references a different claim must not
		// serve as mis-pin proof — otherwise a forged claim pre-pointed
		// at an arbitrary local PV could waive Gate 3.
		for name, mutate := range map[string]func(*corev1.PersistentVolume){
			"nil ClaimRef":   func(pv *corev1.PersistentVolume) { pv.Spec.ClaimRef = nil },
			"wrong name":     func(pv *corev1.PersistentVolume) { pv.Spec.ClaimRef.Name = "some-other-claim" },
			"mismatched UID": func(pv *corev1.PersistentVolume) { pv.Spec.ClaimRef.UID = "uid-of-someone-else" },
		} {
			t.Run(name, func(t *testing.T) {
				pod, datadir, shadow, pv := stuckBroker("rp-1")
				mutate(pv)
				r := newController(t, s, wffc, pod, datadir, shadow, pv)

				res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
				require.NoError(t, err)
				require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a PV without a matching ClaimRef back-reference must not prove a mis-pin")

				// Nothing was touched.
				var gotPVC corev1.PersistentVolumeClaim
				require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
				var gotPod corev1.Pod
				require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
			})
		}
	})

	t.Run("exemption paper trail is not recorded while the freed-pv gate still defers", func(t *testing.T) {
		// The freed-pv gate (Gate 4) runs AFTER the exemption and its
		// durable annotation can hold for days. The exempted
		// metric/Event claim "proceeded past the pvc-rebinding gate" —
		// recording them on a reconcile that then defers at freed-pv
		// would count a gate pass every 30s while nothing proceeds.
		pod, datadir, shadow, pv := stuckBroker("rp-1")
		freed := pvWithAnnotations("pv-freed", corev1.VolumeAvailable, "node-live", map[string]string{
			FreedPVAnnotation: "/ns/redpanda",
		})
		recorder := &events.FakeRecorder{Events: make(chan string, 8)}
		r := newController(t, s, wffc, pod, datadir, shadow, pv, freed, newNode("node-live"))
		r.Recorder = recorder

		exemptedBefore := promtestutil.ToFloat64(observability.PVCUnbinderGateExempted)
		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "the freed-pv gate must still defer")
		require.Equal(t, exemptedBefore, promtestutil.ToFloat64(observability.PVCUnbinderGateExempted), "the exempted metric must not count a gate pass that never proceeded")

		// The paper trail names the freed-pv deferral, never a gate
		// pass that didn't happen.
		close(recorder.Events)
		for ev := range recorder.Events {
			require.NotContains(t, ev, eventReasonGateExempted, "no exemption event may be recorded on a reconcile that defers at the freed-pv gate")
		}

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("sibling stuck on a generic scheduling failure without a mis-pinned bound claim still defers", func(t *testing.T) {
		// The weak signature (schedulingFailureRE) alone is not enough
		// to grant the exemption: a sibling Pending on "0/N nodes are
		// available: ... unbound ... PersistentVolumeClaims" could
		// equally be stuck on an unrelated storage-class, provisioner,
		// or quota failure rather than the provably-deadlocked
		// mis-pinned-bound-claim pattern. Without a Bound claim on a
		// HostPath/Local PV with NodeAffinity to prove the deadlock,
		// Gate 3 must keep deferring.
		pod1, datadir1, shadow1, pv1 := stuckBroker("rp-1")
		sibling := withPVC(newPod("rp-0", "ns", "redpanda"), "datadir-rp-0")
		sibling.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 pod has unbound immediate PersistentVolumeClaims.",
		}}
		siblingClaim := newPVC("datadir-rp-0", "ns", "redpanda", "")
		r := newController(t, s, wffc, pod1, datadir1, shadow1, pv1, sibling, siblingClaim)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod1)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a sibling with no proven mis-pinned bound claim must still defer the unbind")

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("sibling stuck on a non-affinity total scheduling failure with a mis-pinned bound claim is still exempted", func(t *testing.T) {
		// Pins the deliberate breadth of the *signature* match: modern
		// schedulers prefix every total scheduling failure with "0/N
		// nodes are available:" and don't reliably name volume
		// affinity. A sibling Pending on that weaker message form is
		// still exempted, as long as it also has independent proof of
		// the deadlock shape — a Bound claim on a HostPath/Local PV
		// with NodeAffinity.
		pod1, datadir1, shadow1, pv1 := stuckBroker("rp-1")
		sibling := withPVC(withPVC(newPod("rp-0", "ns", "redpanda"), "datadir-rp-0"), "shadow-index-cache-rp-0")
		sibling.Status.Conditions = []corev1.PodCondition{{
			Type:    corev1.PodScheduled,
			Status:  corev1.ConditionFalse,
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 pod has unbound immediate PersistentVolumeClaims.",
		}}
		siblingClaim := newPVC("datadir-rp-0", "ns", "redpanda", "")
		siblingClaim.Spec.StorageClassName = ptr.To("standard")
		siblingShadow := newPVC("shadow-index-cache-rp-0", "ns", "redpanda", "pv-shadow-rp-0")
		siblingPV := boundHostPathPV("pv-shadow-rp-0", "shadow-index-cache-rp-0", "node-a")
		r := newController(t, s, wffc, pod1, datadir1, shadow1, pv1, sibling, siblingClaim, siblingShadow, siblingPV)

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod1)})
		require.NoError(t, err)
		require.Zero(t, res.RequeueAfter, "a sibling matching the weak stuck signature with a proven mis-pinned bound claim must not defer the unbind")

		// Only rp-1's objects were touched.
		var gotPVC corev1.PersistentVolumeClaim
		err = r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC)
		require.True(t, apierrors.IsNotFound(err))
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-0"}, &gotPVC))
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-0"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-0"}, &gotPod))
	})

	t.Run("sibling whose Unschedulable condition is fresher than r.Timeout still defers", func(t *testing.T) {
		// ShouldRemediate requires a Pod's Unschedulable condition to
		// be at least r.Timeout old before treating it as confirmed
		// stuck rather than a transient scheduler/provisioner hiccup —
		// the reconciled Pod itself is gated on this before Reconcile
		// ever reaches Gate 3. A sibling must earn that same
		// confirmation, not just match the weak signature and have a
		// mis-pinned bound claim: rp-0 here turned Unschedulable only
		// moments ago (LastTransitionTime ~= now, well under
		// r.Timeout), so its unbound claim must NOT be exempted yet —
		// Gate 0 provides no backstop here, since no prior unbind has
		// annotated a PV for a sibling that's merely freshly stuck.
		pod1, datadir1, shadow1, pv1 := stuckBroker("rp-1")
		sibling, siblingDatadir, siblingShadow, siblingPV := stuckBroker("rp-0")
		sibling.Status.Conditions[0].LastTransitionTime = metav1.Now()
		r := newController(t, s, wffc, pod1, datadir1, shadow1, pv1, sibling, siblingDatadir, siblingShadow, siblingPV)
		r.Timeout = 30 * time.Second

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod1)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a sibling fresher than r.Timeout must not be exempted; Gate 3 must keep deferring")

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("sibling excluded by Selector still defers despite a mis-pinned bound claim", func(t *testing.T) {
		// The Selector is an operational boundary: an admin can scope
		// this controller to a subset of Pods. A stuck sibling outside
		// that scope must not be treated as exempt-worthy, or the
		// Selector stops meaningfully limiting which claims Gate 3
		// treats as safe to bypass.
		pod1, datadir1, shadow1, pv1 := stuckBroker("rp-1")
		pod1.Labels["scope"] = "in"
		sibling, siblingDatadir, siblingShadow, siblingPV := stuckBroker("rp-0")
		sibling.Labels["scope"] = "out"
		r := newController(t, s, wffc, pod1, datadir1, shadow1, pv1, sibling, siblingDatadir, siblingShadow, siblingPV)
		r.Selector = labels.SelectorFromSet(labels.Set{"scope": "in"})

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod1)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a Selector-excluded sibling must still defer the unbind")

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "shadow-index-cache-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
	})

	t.Run("sibling unbound claim still defers", func(t *testing.T) {
		// The serialization Gate 3 exists for: another broker's claim
		// is mid-rebind (unbound, owner pod absent or not stuck on
		// volume affinity), so rp-1's unbind must wait. Only claims of
		// provably-stuck pods are exempt.
		pod := withPVC(podWithVolumeAffinityFailure("rp-1", "ns", "redpanda"), "datadir-rp-1")
		mispinned := newPVC("datadir-rp-1", "ns", "redpanda", "pv-data-1")
		pv := boundHostPathPV("pv-data-1", "datadir-rp-1", "node-a")
		sibling := newPVC("datadir-rp-0", "ns", "redpanda", "")
		recorder := &events.FakeRecorder{Events: make(chan string, 8)}
		r := newController(t, s, wffc, pod, mispinned, sibling, pv)
		r.Recorder = recorder

		res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pod)})
		require.NoError(t, err)
		require.Equal(t, requeueDuringDisruption, res.RequeueAfter, "a sibling's unbound claim must still defer the unbind")

		// The pvc-rebinding gate specifically fired (not another gate
		// returning the same requeue), and the event names the claim
		// that gated the unbind.
		select {
		case ev := <-recorder.Events:
			require.Contains(t, ev, "gate="+gatePVCRebinding)
			require.Contains(t, ev, "datadir-rp-0")
		default:
			t.Fatal("expected a PVCUnbinderDeferred event naming the pvc-rebinding gate")
		}

		// Nothing was touched.
		var gotPVC corev1.PersistentVolumeClaim
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "datadir-rp-1"}, &gotPVC))
		var gotPod corev1.Pod
		require.NoError(t, r.Client.Get(ctx, client.ObjectKey{Namespace: "ns", Name: "rp-1"}, &gotPod))
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
			require.Equal(t, tc.want, PodHasVolumeAffinityUnschedulable(pod))
		})
	}

	t.Run("no PodScheduled condition", func(t *testing.T) {
		require.False(t, PodHasVolumeAffinityUnschedulable(&corev1.Pod{}))
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
// in pod name, matching StsPVCs() suffix-detection). The volume is
// named after the claim so that pods built with multiple calls remain
// valid API objects (volume names must be unique within a pod).
func withPVC(p *corev1.Pod, claimName string) *corev1.Pod {
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{
		Name: claimName,
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
			require.Equal(t, tc.want, NodeFromPVAffinity(tc.pv))
		})
	}
}

// nodeSelectorTerm is a small helper for building
// corev1.NodeSelectorTerm literals below without repeating the nested
// struct shape.
func nodeSelectorTerm(exprs ...corev1.NodeSelectorRequirement) corev1.NodeSelectorTerm {
	return corev1.NodeSelectorTerm{MatchExpressions: exprs}
}

// hostnameIn builds the standard "kubernetes.io/hostname In [values]"
// NodeSelectorRequirement Local/HostPath PVs use.
func hostnameIn(values ...string) corev1.NodeSelectorRequirement {
	return corev1.NodeSelectorRequirement{
		Key:      corev1.LabelHostname,
		Operator: corev1.NodeSelectorOpIn,
		Values:   values,
	}
}

// TestPVPinnedHostnames verifies the strict (fail-closed) NodeAffinity
// resolver backing podHasMispinnedBoundClaim's mis-pin proof — unlike
// nodeFromPVAffinity (which nearby TestNodeFromPVAffinity covers),
// this one must resolve the FULL eligible-Node set or explicitly
// refuse, since collapsing to a partial set here would risk unbinding
// a claim that could still bind on a Node it silently dropped.
func TestPVPinnedHostnames(t *testing.T) {
	cases := []struct {
		name   string
		pv     *corev1.PersistentVolume
		want   []string
		wantOK bool
	}{
		{
			name:   "single term, single value resolves",
			pv:     newPVWithAffinity("pv", "ns", "claim", "node-a"),
			want:   []string{"node-a"},
			wantOK: true,
		},
		{
			name: "single term, multiple values all resolve",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(hostnameIn("node-a", "node-b")),
						},
					},
				},
			}},
			want:   []string{"node-a", "node-b"},
			wantOK: true,
		},
		{
			name: "multiple OR'd terms union their hostnames",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(hostnameIn("node-a")),
							nodeSelectorTerm(hostnameIn("node-b")),
						},
					},
				},
			}},
			want:   []string{"node-a", "node-b"},
			wantOK: true,
		},
		{
			name:   "no NodeAffinity fails closed",
			pv:     &corev1.PersistentVolume{},
			wantOK: false,
		},
		{
			name:   "NodeAffinity without Required fails closed",
			pv:     &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{NodeAffinity: &corev1.VolumeNodeAffinity{}}},
			wantOK: false,
		},
		{
			name: "empty NodeSelectorTerms fails closed",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{}},
			}},
			wantOK: false,
		},
		{
			name: "a term with an additional co-ANDed expression fails closed (can't know the real eligible set)",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(
								hostnameIn("node-a"),
								corev1.NodeSelectorRequirement{
									Key:      "topology.kubernetes.io/zone",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"us-east-1a"},
								},
							),
						},
					},
				},
			}},
			wantOK: false,
		},
		{
			name: "a term with MatchFields fails closed",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{
							MatchExpressions: []corev1.NodeSelectorRequirement{hostnameIn("node-a")},
							MatchFields: []corev1.NodeSelectorRequirement{{
								Key:      "metadata.name",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"node-a"},
							}},
						}},
					},
				},
			}},
			wantOK: false,
		},
		{
			name: "non-hostname key fails closed (e.g. zone topology)",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(corev1.NodeSelectorRequirement{
								Key:      "topology.kubernetes.io/zone",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"us-east-1a"},
							}),
						},
					},
				},
			}},
			wantOK: false,
		},
		{
			name: "hostname NotIn operator fails closed (only In is a positive enumeration)",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(corev1.NodeSelectorRequirement{
								Key:      corev1.LabelHostname,
								Operator: corev1.NodeSelectorOpNotIn,
								Values:   []string{"node-a"},
							}),
						},
					},
				},
			}},
			wantOK: false,
		},
		{
			name: "hostname In with no values fails closed",
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(hostnameIn()),
						},
					},
				},
			}},
			wantOK: false,
		},
		{
			name: "one resolvable term plus one unresolvable term fails closed for the WHOLE PV",
			// Terms are OR'd: if the unresolvable term's real eligible
			// set is unknown, the PV's true eligible-Node set can't be
			// bounded even though the OTHER term resolved fine.
			pv: &corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
				NodeAffinity: &corev1.VolumeNodeAffinity{
					Required: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							nodeSelectorTerm(hostnameIn("node-a")),
							nodeSelectorTerm(corev1.NodeSelectorRequirement{
								Key:      "topology.kubernetes.io/zone",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"us-east-1a"},
							}),
						},
					},
				},
			}},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pvPinnedHostnames(tc.pv)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.Equal(t, tc.want, got)
			}
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
		got, err := r.listClusterPVCsByName(ctx, r.Client, pod)
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
		got, err := r.listClusterPVCsByName(ctx, r.Client, pod)
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
		got, err := r.listClusterPVCsByName(ctx, r.Client, pod)
		require.NoError(t, err)
		require.Equal(t, "pv-0", got["datadir-rp-0"].Spec.VolumeName)
		require.Equal(t, "", got["datadir-rp-1"].Spec.VolumeName)
	})

	t.Run("PVC in different namespace is excluded", func(t *testing.T) {
		pod := newPod("rp-0", "ns-a", "redpanda")
		other := newPVC("datadir-rp-0", "ns-b", "redpanda", "pv-0")
		r := newController(t, s, other)
		got, err := r.listClusterPVCsByName(ctx, r.Client, pod)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("pod without instance label returns empty (non-nil) map", func(t *testing.T) {
		pod := newPod("orphan-0", "ns", "")
		other := newPVC("datadir-other-0", "ns", "redpanda", "pv-0")
		r := newController(t, s, other)
		got, err := r.listClusterPVCsByName(ctx, r.Client, pod)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}

// TestClaimListForEvent pins the Event-note cap: events.k8s.io/v1
// rejects notes over 1024 characters and the broadcaster silently
// drops the rejected Event, so large exempted-claim lists must be
// truncated for Events (logs keep the full list).
func TestClaimListForEvent(t *testing.T) {
	short := []string{"datadir-rp-0", "datadir-rp-1"}
	require.Equal(t, "[datadir-rp-0 datadir-rp-1]", claimListForEvent(short))

	var long []string
	for i := range 40 {
		long = append(long, fmt.Sprintf("shadow-index-cache-redpanda-cluster-%d", i))
	}
	capped := claimListForEvent(long)
	require.Contains(t, capped, "(+32 more)")
	require.Less(t, len(capped), 1024, "a capped note must fit the events.k8s.io/v1 1024-character limit")
	msg := fmt.Sprintf("unbound claims %s are exempted as stuck-Pod claims and the reconciled Pod holds its own mis-pin proof; proceeding past the pvc-rebinding gate", capped)
	require.Less(t, len(msg), 1024, "the full Event note must fit the limit even with the surrounding message")

	// A count cap alone would not bound the note: claim names can
	// legally reach 253 characters, so the cap must also budget total
	// rendered length.
	var maximal []string
	for i := range 10 {
		maximal = append(maximal, fmt.Sprintf("%0253d", i))
	}
	capped = claimListForEvent(maximal)
	msg = fmt.Sprintf("unbound claims %s are exempted as stuck-Pod claims and the reconciled Pod holds its own mis-pin proof; proceeding past the pvc-rebinding gate", capped)
	require.Less(t, len(msg), 1024, "maximal-length claim names must still fit the Event note limit")
	require.Contains(t, capped, "more)", "the omitted-name count must still be reported")
}

// TestLostDiskClaimsResolvesNodeByHostnameLabel proves node liveness is
// judged by the kubernetes.io/hostname LABEL, not the Node object name:
// PV NodeAffinity carries the label value, and under kubelet
// --hostname-override the two differ — a name-based Get would report a
// live node as gone and authorize the terminal DiskLost marking of a
// healthy broker. It also proves the proof is READ-ONLY: reclaim policies
// are never touched, and claims are collected regardless of who
// provisioned them (migrated brokers reference ExistingClaims that must
// still qualify).
func TestLostDiskClaimsResolvesNodeByHostnameLabel(t *testing.T) {
	ctx := context.Background()
	s := newScheme(t, false, false, false)

	hostPathPV := func(name, claimName, hostname string) *corev1.PersistentVolume {
		pv := newPVWithAffinity(name, "ns", claimName, hostname)
		pv.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/data"},
		}
		pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
		pv.Status.Phase = corev1.VolumeBound
		return pv
	}

	pod := withPVC(withPVC(newPod("rp-0", "ns", "redpanda"), "datadir-rp-0"), "existing-rp-0")

	// The node's OBJECT name differs from its hostname label — the
	// --hostname-override shape.
	overriddenNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "ip-10-0-0-1.ec2.internal",
		Labels: map[string]string{corev1.LabelHostname: "live-hostname"},
	}}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		overriddenNode,
		newPVC("datadir-rp-0", "ns", "redpanda", "pv-live"),
		hostPathPV("pv-live", "datadir-rp-0", "live-hostname"),
		newPVC("existing-rp-0", "ns", "redpanda", "pv-dead"),
		hostPathPV("pv-dead", "existing-rp-0", "dead-hostname"),
	).Build()

	// One claim on a live (hostname-overridden) node, one on a genuinely
	// absent hostname: only the dead one is proof, and every reclaim policy
	// stays untouched — the proof mutates nothing.
	lost, err := LostDiskClaims(ctx, c, pod)
	require.NoError(t, err)
	require.Len(t, lost, 1, "a live node must not be treated as dead just because its object name differs from its hostname label")
	require.Equal(t, "existing-rp-0", lost[0].Name)
	for _, pvName := range []string{"pv-live", "pv-dead"} {
		var pv corev1.PersistentVolume
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: pvName}, &pv))
		require.Equal(t, corev1.PersistentVolumeReclaimDelete, pv.Spec.PersistentVolumeReclaimPolicy, "the proof must be read-only")
	}

	// Unbound claim and non-local PV: never proof.
	unbound := withPVC(newPod("rp-2", "ns", "redpanda"), "datadir-rp-2")
	nonLocalPV := newPVWithAffinity("pv-nfs", "ns", "datadir-rp-2", "gone-hostname")
	nonLocalPV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{
		NFS: &corev1.NFSVolumeSource{Server: "nfs", Path: "/data"},
	}
	c = fake.NewClientBuilder().WithScheme(s).WithObjects(
		newPVC("datadir-rp-2", "ns", "redpanda", "pv-nfs"),
		nonLocalPV,
	).Build()
	lost, err = LostDiskClaims(ctx, c, unbound)
	require.NoError(t, err)
	require.Empty(t, lost, "network-attached storage is never disk-loss proof")
}
