// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

var _ Resource = &BrokerSetResource{}

// BrokerSetResource manages Broker CRs for a single node pool, replacing
// StatefulSetResource when the cluster has the migrate-to-broker-cr annotation.
type BrokerSetResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster
	// stsResource is a StatefulSetResource used purely for rendering — its
	// obj() method produces the canonical pod template that we convert into
	// Broker CRs. The STS is never created.
	stsResource *StatefulSetResource
	nodePool    vectorizedv1alpha1.NodePoolSpecWithDeleted
	logger      logr.Logger
}

// NewBrokerSet creates a BrokerSetResource that renders Broker CRs by first
// producing a StatefulSet via the existing rendering pipeline, then converting
// the STS spec into individual Broker CRs.
func NewBrokerSet(
	client k8sclient.Client,
	pandaCluster *vectorizedv1alpha1.Cluster,
	scheme *runtime.Scheme,
	serviceFQDN string,
	serviceName string,
	nodePortName types.NamespacedName,
	volumeProvider resourcetypes.StatefulsetTLSVolumeProvider,
	adminTLSConfigProvider resourcetypes.AdminTLSConfigProvider,
	serviceAccountName string,
	configuratorSettings ConfiguratorSettings,
	cfg *clusterconfiguration.CombinedCfg,
	adminAPIClientFactory adminutils.NodePoolAdminAPIClientFactory,
	dialer redpanda.DialContextFunc,
	decommissionWaitInterval time.Duration,
	logger logr.Logger,
	metricsTimeout time.Duration,
	nodePool vectorizedv1alpha1.NodePoolSpecWithDeleted,
	autoDeletePVCs bool,
	brokerPodNodeUnavailableToleration time.Duration,
) *BrokerSetResource {
	sts := NewStatefulSet(
		client, pandaCluster, scheme,
		serviceFQDN, serviceName, nodePortName,
		volumeProvider, adminTLSConfigProvider,
		serviceAccountName, configuratorSettings, cfg,
		adminAPIClientFactory, dialer, decommissionWaitInterval,
		logger, metricsTimeout, nodePool, autoDeletePVCs,
		brokerPodNodeUnavailableToleration,
	)
	return &BrokerSetResource{
		Client:       client,
		scheme:       scheme,
		pandaCluster: pandaCluster,
		stsResource:  sts,
		nodePool:     nodePool,
		logger:       logger.WithName("BrokerSetResource"),
	}
}

func (r *BrokerSetResource) Key() types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("brokerset-%s", r.nodePool.Name),
		Namespace: r.pandaCluster.Namespace,
	}
}

func (r *BrokerSetResource) Ensure(ctx context.Context) error {
	l := r.logger.WithValues("nodepool", r.nodePool.Name)

	existingSTS, err := r.getExistingStatefulSet(ctx)
	if err != nil {
		return fmt.Errorf("checking for existing StatefulSet: %w", err)
	}

	if existingSTS != nil {
		return r.ensureMigration(ctx, l, existingSTS)
	}

	return r.ensureBrokers(ctx, l)
}

// ensureMigration runs the StatefulSet→Broker CR migration state machine.
//
// State 0→1: Create shadow Broker CRs + back up STS spec to ConfigMap.
// State 1→2: Verify PVC retention, strip ownerRefs, orphan-delete STS.
//
// The Broker controller handles pod adoption once pods are orphaned.
//
// Every pass first re-verifies the migration preconditions: the migration
// must never start (or progress to the destructive step) while a rollout is
// pending or the cluster is anything but healthy and stable.
func (r *BrokerSetResource) ensureMigration(ctx context.Context, l logr.Logger, sts *appsv1.StatefulSet) error {
	l.Info("migration: StatefulSet exists, running state machine", "sts", sts.Name)

	stsReplicas := int(ptr.Deref(sts.Spec.Replicas, 0))

	// Render shadow Brokers from the desired STS (obj()), not the live one.
	// This ensures the configmap checksum matches what ensureBrokers will
	// compute post-migration, preventing spurious pod rotation during adoption.
	desiredSTS, err := r.stsResource.obj(ctx)
	if err != nil {
		return fmt.Errorf("rendering desired StatefulSet for migration: %w", err)
	}
	desired := desiredSTS.(*appsv1.StatefulSet)
	desired.Spec.Replicas = ptr.To(int32(stsReplicas))

	if err := r.verifyMigrationPreconditions(ctx, l, sts, desired); err != nil {
		return err
	}

	shadowBrokers, err := brokersFromStatefulSet(r.pandaCluster, desired, r.nodePool, r.scheme, true)
	if err != nil {
		return fmt.Errorf("rendering shadow Broker CRs: %w", err)
	}

	existing, err := r.listBrokers(ctx)
	if err != nil {
		return fmt.Errorf("listing Broker CRs: %w", err)
	}
	existingByIndex := indexBrokers(existing)

	// State 0→1: create shadow Broker CRs for missing ordinals.
	for i := range shadowBrokers {
		d := &shadowBrokers[i]
		idx := *d.Spec.NetworkIndex
		if _, ok := existingByIndex[idx]; !ok {
			d.GenerateName = r.stsResource.Key().Name + "-"
			l.Info("migration: creating shadow Broker CR", "generateName", d.GenerateName, "index", idx)
			if err := r.Create(ctx, d); err != nil {
				return fmt.Errorf("creating shadow Broker ordinal %d: %w", idx, err)
			}
		}
	}

	if err := r.ensureBackupConfigMap(ctx, l, sts); err != nil {
		return fmt.Errorf("backing up StatefulSet: %w", err)
	}

	// Re-list to confirm all shadow Brokers exist.
	existing, err = r.listBrokers(ctx)
	if err != nil {
		return err
	}
	if len(existing) < stsReplicas {
		l.Info("migration: waiting for all shadow Broker CRs", "have", len(existing), "want", stsReplicas)
		setMigrationCondition(ctx, r.Client, r.pandaCluster, l, corev1.ConditionFalse,
			vectorizedv1alpha1.BrokerMigrationReasonInProgress, "creating shadow Broker CRs")
		return nil
	}

	// State 1→2: verify retention, orphan-delete STS.
	// We rely solely on orphan propagation to strip the STS ownerRef from
	// pods and PVCs. Manually stripping ownerRefs before deletion creates a
	// race where the STS controller re-adopts pods in the gap, which can
	// lead to pod deletion.
	if err := verifyPVCRetention(sts); err != nil {
		return fmt.Errorf("migration precondition failed: %w", err)
	}

	l.Info("migration: orphan-deleting StatefulSet", "name", sts.Name)
	if err := r.Delete(ctx, sts, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("orphan-deleting StatefulSet %s: %w", sts.Name, err)
	}

	l.Info("migration: StatefulSet deleted, GC will strip ownerRefs, Broker controller will adopt pods")
	setMigrationCondition(ctx, r.Client, r.pandaCluster, l, corev1.ConditionFalse,
		vectorizedv1alpha1.BrokerMigrationReasonInProgress, "StatefulSet deleted; Broker controller adopting pods")
	return nil
}

// verifyMigrationPreconditions blocks the migration state machine until the
// cluster is quiescent: the live StatefulSet has fully rolled out, every pod
// is ready and running the DESIRED configuration, no restart or decommission
// is in flight, and the cluster reports healthy via the admin API.
//
// The desired-config check is what makes adoption safe: shadow Brokers are
// rendered from the desired spec and adoption marks pods as current, so a
// config rollout pending at migration time would otherwise be silently
// cancelled.
func (r *BrokerSetResource) verifyMigrationPreconditions(ctx context.Context, l logr.Logger, liveSTS, desiredSTS *appsv1.StatefulSet) error {
	block := func(reason string) error {
		l.Info("migration blocked, waiting for the cluster to become stable", "reason", reason)
		setMigrationCondition(ctx, r.Client, r.pandaCluster, l, corev1.ConditionFalse,
			vectorizedv1alpha1.BrokerMigrationReasonBlocked, reason)
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "migration blocked: " + reason,
		}
	}

	if r.pandaCluster.Status.IsRestarting() {
		return block("cluster is restarting")
	}
	if r.pandaCluster.Status.DecommissioningNode != nil {
		return block(fmt.Sprintf("decommission of node_id=%d in progress", *r.pandaCluster.Status.DecommissioningNode))
	}

	// Revision-based signals (UpdatedReplicas, CurrentRevision vs
	// UpdateRevision) are intentionally not consulted here. The operator's
	// StatefulSets use the OnDelete update strategy, and after a rollback the
	// StatefulSet adopts Broker-controller-created pods that carry no
	// controller-revision-hash label — under OnDelete kube never re-labels or
	// rolls them, so those counters never converge and would block
	// re-migration forever. The per-pod readiness and desired-config checks
	// below are the actual rollout-completeness signal.
	specReplicas := ptr.Deref(liveSTS.Spec.Replicas, 0)
	st := liveSTS.Status
	if st.ObservedGeneration != liveSTS.Generation ||
		st.Replicas != specReplicas ||
		st.ReadyReplicas != specReplicas {
		return block("StatefulSet rollout has not completed")
	}

	desiredHash := desiredSTS.Spec.Template.Annotations[ConfigMapHashAnnotationKey]
	if liveSTS.Spec.Template.Annotations[ConfigMapHashAnnotationKey] != desiredHash {
		return block("config change pending, StatefulSet not yet updated")
	}

	pods, err := r.stsResource.getPodList(ctx)
	if err != nil {
		return fmt.Errorf("listing pods for migration preconditions: %w", err)
	}
	if int32(len(pods.Items)) != specReplicas {
		return block(fmt.Sprintf("expected %d pods, found %d", specReplicas, len(pods.Items)))
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !utils.IsPodReady(pod) {
			return block(fmt.Sprintf("pod %s is not ready", pod.Name))
		}
		if pod.Annotations[ConfigMapHashAnnotationKey] != desiredHash {
			return block(fmt.Sprintf("pod %s is not running the desired configuration", pod.Name))
		}
	}

	// Finally the cluster itself must be healthy; isClusterHealthy returns a
	// RequeueAfterError when it is not.
	return r.stsResource.isClusterHealthy(ctx)
}

// ensureBrokers is the steady-state broker lifecycle (new cluster or post-migration).
func (r *BrokerSetResource) ensureBrokers(ctx context.Context, l logr.Logger) error {
	// Migration completion is observed, not recorded: reaching steady state
	// (no StatefulSet left) IS completion. Only progress an existing
	// condition — clusters that never migrated don't get one.
	if cond := r.pandaCluster.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType); cond != nil &&
		cond.Reason != vectorizedv1alpha1.BrokerMigrationReasonComplete {
		setMigrationCondition(ctx, r.Client, r.pandaCluster, l, corev1.ConditionTrue,
			vectorizedv1alpha1.BrokerMigrationReasonComplete, "StatefulSet removed; Broker CRs manage all pods")
	}

	desired, err := r.desiredBrokers(ctx)
	if err != nil {
		return fmt.Errorf("rendering desired Broker CRs: %w", err)
	}

	existing, err := r.listBrokers(ctx)
	if err != nil {
		return fmt.Errorf("listing existing Broker CRs: %w", err)
	}

	existingByIndex := indexBrokers(existing)
	desiredReplicas := ptr.Deref(r.nodePool.Replicas, 0)

	decommissionInFlight := slices.ContainsFunc(existing, func(b redpandav1alpha2.Broker) bool {
		return b.Spec.Decommission && b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned
	})

	for i := range desired {
		d := &desired[i]
		idx := *d.Spec.NetworkIndex

		existingBroker, ok := existingByIndex[idx]
		if !ok {
			if err := r.createBroker(ctx, l, d); err != nil {
				return err
			}
			continue
		}
		delete(existingByIndex, idx)

		if err := r.ensureDesiredBroker(ctx, l, existingBroker, d); err != nil {
			return err
		}
	}

	if err := r.reconcileExcessBrokers(ctx, l, existingByIndex, desiredReplicas, decommissionInFlight); err != nil {
		return err
	}

	return r.ensureRollGrants(ctx, l)
}

// ensureDesiredBroker reconciles one desired Broker against the existing CR
// at the same index.
//
// Decommission intent — whether set by scale-down or manually by a human —
// is never unset by the operator (recommission was dropped from RFC Q2): a
// broker with the intent set is left alone until the decommission completes.
// Once terminal, the CR is deleted so the next pass creates a fresh Broker
// (and node identity) for the still-desired index. Re-creation waits for the
// old CR to be fully gone so its deletion handling cannot race the
// replacement's identically-named pod.
func (r *BrokerSetResource) ensureDesiredBroker(ctx context.Context, l logr.Logger, existingBroker *redpandav1alpha2.Broker, d *redpandav1alpha2.Broker) error {
	if existingBroker.Spec.Decommission {
		if existingBroker.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned && existingBroker.DeletionTimestamp.IsZero() {
			l.Info("replacing decommissioned Broker at desired index", "name", existingBroker.Name, "index", *existingBroker.Spec.NetworkIndex)
			if err := r.Delete(ctx, existingBroker); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting decommissioned Broker %s: %w", existingBroker.Name, err)
			}
		}
		return nil
	}

	return r.updateBroker(ctx, l, existingBroker, d)
}

// reconcileExcessBrokers drives scale-down: excess brokers (index >=
// desiredReplicas, highest first) are marked for decommission strictly one at
// a time — any decommission already in flight (including a manual one on a
// desired index) blocks marking the next one — and deleted once they reach
// the Decommissioned phase.
func (r *BrokerSetResource) reconcileExcessBrokers(ctx context.Context, l logr.Logger, existingByIndex map[int32]*redpandav1alpha2.Broker, desiredReplicas int32, decommissionInFlight bool) error {
	var excess []*redpandav1alpha2.Broker
	for idx, b := range existingByIndex {
		if idx >= desiredReplicas {
			excess = append(excess, b)
		}
	}
	sort.Slice(excess, func(i, j int) bool {
		return ptr.Deref(excess[i].Spec.NetworkIndex, 0) > ptr.Deref(excess[j].Spec.NetworkIndex, 0)
	})

	for _, b := range excess {
		if b.Status.Phase == redpandav1alpha2.BrokerPhaseDecommissioned {
			l.Info("deleting decommissioned Broker CR", "name", b.Name)
			if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting decommissioned Broker %s: %w", b.Name, err)
			}
			continue
		}
		if decommissionInFlight {
			// A decommission is already in flight (here or anywhere in the
			// pool) — never start a second one; wait for it to complete.
			break
		}
		l.Info("marking Broker for decommission (scale-down)", "name", b.Name, "index", *b.Spec.NetworkIndex)
		b.Spec.Decommission = true
		if err := r.Update(ctx, b); err != nil {
			return fmt.Errorf("setting decommission on Broker %s: %w", b.Name, err)
		}
		break // one at a time
	}

	return nil
}

func (r *BrokerSetResource) desiredBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	stsObj, err := r.stsResource.obj(ctx)
	if err != nil {
		return nil, fmt.Errorf("rendering StatefulSet: %w", err)
	}
	if stsObj == nil {
		return nil, nil
	}
	sts := stsObj.(*appsv1.StatefulSet)

	return brokersFromStatefulSet(r.pandaCluster, sts, r.nodePool, r.scheme, false)
}

// brokersFromStatefulSet converts a StatefulSet spec into Broker CRs, one per
// ordinal. When migration=false (new cluster), Brokers use VolumeClaimTemplates.
// When migration=true (STS→Broker migration), Brokers use ExistingClaims to
// reference StatefulSet-created PVCs and the replica count comes from the live STS.
func brokersFromStatefulSet(
	cluster *vectorizedv1alpha1.Cluster,
	sts *appsv1.StatefulSet,
	nodePool vectorizedv1alpha1.NodePoolSpecWithDeleted,
	scheme *runtime.Scheme,
	migration bool,
) ([]redpandav1alpha2.Broker, error) {
	replicas := ptr.Deref(nodePool.Replicas, 0)
	if migration {
		replicas = ptr.Deref(sts.Spec.Replicas, 0)
	}
	stsName := sts.Name
	nodePoolLabels := labels.ForCluster(cluster).WithNodePool(nodePool.Name)

	configHash := sts.Spec.Template.Annotations[ConfigMapHashAnnotationKey]

	vctNames := map[string]bool{}
	for _, vct := range sts.Spec.VolumeClaimTemplates {
		vctNames[vct.Name] = true
	}

	var brokerVCTs []redpandav1alpha2.BrokerVolumeClaim
	if !migration {
		for _, vct := range sts.Spec.VolumeClaimTemplates {
			brokerVCTs = append(brokerVCTs, redpandav1alpha2.BrokerVolumeClaim{
				Name: vct.Name,
				Spec: vct.Spec,
			})
		}
	}

	var brokers []redpandav1alpha2.Broker
	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)

		podSpec := *sts.Spec.Template.Spec.DeepCopy()
		podSpec.Hostname = podName
		podSpec.Subdomain = cluster.Name
		normalizePodSpecDefaults(&podSpec)

		for vi := range podSpec.Volumes {
			v := &podSpec.Volumes[vi]
			if v.PersistentVolumeClaim != nil && vctNames[v.PersistentVolumeClaim.ClaimName] {
				v.PersistentVolumeClaim.ClaimName = fmt.Sprintf("%s-%s", v.PersistentVolumeClaim.ClaimName, podName)
			}
		}

		podAnnotations := maps.Clone(sts.Spec.Template.Annotations)
		podAnnotations[redpandav1alpha2.BrokerConfigChecksumAnnotation] = configHash

		brokerLabels := maps.Clone(nodePoolLabels)
		brokerLabels["cluster.redpanda.com/network-index"] = fmt.Sprintf("%d", i)
		// ClusterNameLabel must be set on every Broker CR: Broker.PodName()
		// relies on it to recover the cluster half of the pod name when the
		// ClusterRef names a NodePool rather than the cluster.
		brokerLabels[redpandav1alpha2.ClusterNameLabel] = cluster.Name

		storage := redpandav1alpha2.BrokerStorage{
			VolumeClaimTemplates: brokerVCTs,
		}
		if migration {
			var claims []redpandav1alpha2.ExistingClaim
			for _, vct := range sts.Spec.VolumeClaimTemplates {
				claims = append(claims, redpandav1alpha2.ExistingClaim{
					Name: fmt.Sprintf("%s-%s", vct.Name, podName),
				})
			}
			storage = redpandav1alpha2.BrokerStorage{
				ExistingClaims: claims,
			}
		}

		// Propagate the deletion policy so it stays readable during cluster
		// teardown, after the Cluster object itself is gone.
		var brokerAnnotations map[string]string
		if policy, ok := cluster.Annotations[feature.BrokerDeletionPolicy.Key]; ok {
			brokerAnnotations = map[string]string{feature.BrokerDeletionPolicy.Key: policy}
		}

		broker := redpandav1alpha2.Broker{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   cluster.Namespace,
				Labels:      brokerLabels,
				Annotations: brokerAnnotations,
			},
			Spec: redpandav1alpha2.BrokerSpec{
				ClusterRef: redpandav1alpha2.ClusterRef{
					Group: ptr.To("redpanda.vectorized.io"),
					Kind:  ptr.To("Cluster"),
					Name:  cluster.Name,
				},
				NetworkIndex: ptr.To(i),
				PodTemplate: redpandav1alpha2.BrokerPodTemplate{
					Labels:      brokerLabels,
					Annotations: podAnnotations,
					Spec:        podSpec,
				},
				Storage: storage,
			},
		}

		if err := controllerutil.SetControllerReference(cluster, &broker, scheme); err != nil {
			return nil, fmt.Errorf("setting owner reference on Broker ordinal %d: %w", i, err)
		}

		brokers = append(brokers, broker)
	}

	return brokers, nil
}

// normalizePodSpecDefaults aligns the rendered pod spec with the values the
// Broker CRD's structural defaulting applies on write (container port
// protocol defaults to TCP). Without this the in-memory render never compares
// equal to the stored object — the API server re-adds the default on every
// write — and each reconcile issues a pointless no-op update.
func normalizePodSpecDefaults(spec *corev1.PodSpec) {
	defaultPorts := func(containers []corev1.Container) {
		for i := range containers {
			for j := range containers[i].Ports {
				if containers[i].Ports[j].Protocol == "" {
					containers[i].Ports[j].Protocol = corev1.ProtocolTCP
				}
			}
		}
	}
	defaultPorts(spec.InitContainers)
	defaultPorts(spec.Containers)
}

func (r *BrokerSetResource) listBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	nodePoolLabels := labels.ForCluster(r.pandaCluster).WithNodePool(r.nodePool.Name)
	var list redpandav1alpha2.BrokerList
	if err := r.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: nodePoolLabels.AsClientSelectorForNodePool(),
		Namespace:     r.pandaCluster.Namespace,
	}); err != nil {
		return nil, err
	}
	// Filter to only those owned by this cluster.
	var owned []redpandav1alpha2.Broker
	for _, b := range list.Items {
		if metav1.IsControlledBy(&b, r.pandaCluster) {
			owned = append(owned, b)
		}
	}
	return owned, nil
}

func (r *BrokerSetResource) createBroker(ctx context.Context, l logr.Logger, desired *redpandav1alpha2.Broker) error {
	desired.GenerateName = r.stsResource.Key().Name + "-"

	l.Info("creating Broker CR", "generateName", desired.GenerateName, "index", *desired.Spec.NetworkIndex)
	return r.Create(ctx, desired)
}

func (r *BrokerSetResource) updateBroker(ctx context.Context, l logr.Logger, existing *redpandav1alpha2.Broker, desired *redpandav1alpha2.Broker) error {
	// Preserve the restart marker: it is stamped by MarkBrokersForRestart,
	// not by the renderer, and must survive PodTemplate syncs until the
	// restart has rolled through.
	restartKey := redpandav1alpha2.BrokerClusterConfigVersionAnnotation
	if v, ok := existing.Spec.PodTemplate.Annotations[restartKey]; ok {
		if desired.Spec.PodTemplate.Annotations == nil {
			desired.Spec.PodTemplate.Annotations = map[string]string{}
		}
		if _, set := desired.Spec.PodTemplate.Annotations[restartKey]; !set {
			desired.Spec.PodTemplate.Annotations[restartKey] = v
		}
	}

	policyKey := feature.BrokerDeletionPolicy.Key
	desiredPolicy, desiredPolicySet := desired.Annotations[policyKey]
	existingPolicy, existingPolicySet := existing.Annotations[policyKey]
	policyChanged := desiredPolicy != existingPolicy || desiredPolicySet != existingPolicySet

	if equality.Semantic.DeepEqual(existing.Spec.PodTemplate, desired.Spec.PodTemplate) &&
		!policyChanged {
		return nil
	}
	// Spec.Decommission is deliberately not synced: decommission intent is
	// never unset by the operator, not even when the index is desired again.
	// Terminal brokers at desired indices are replaced by ensureDesiredBroker.
	existing.Spec.PodTemplate = desired.Spec.PodTemplate
	if desiredPolicySet {
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[policyKey] = desiredPolicy
	} else {
		delete(existing.Annotations, policyKey)
	}

	l.V(1).Info("updating Broker CR", "name", existing.Name, "index", *existing.Spec.NetworkIndex)
	return r.Update(ctx, existing)
}

// ensureRollGrants serializes disruptive pod actions across the whole cluster
// (all node pools): at most one Broker holds a valid roll-grant at any time.
// The cluster controller is the single writer of the annotation — it grants
// (health-gated) and revokes; the Broker controller only reads it.
//
// Grant lifecycle per reconcile:
//  1. Revoke grants that completed (pod matches desired checksum, is ready,
//     and the broker is registered) or went stale (checksum mismatch after a
//     config change mid-roll).
//  2. If any unexpired grant remains, wait — never double-grant. Expired
//     grants are treated as released (feature.RollGrantTTL is a safety valve
//     against controller restarts and wedged rolls).
//  3. Hold off while any decommission is in flight: one disruptive operation
//     at a time.
//  4. Grant the first Broker that needs a roll (outdated pod or Stuck phase),
//     preferring a broker with an expired grant (mid-roll), only when the
//     cluster is healthy.
func (r *BrokerSetResource) ensureRollGrants(ctx context.Context, l logr.Logger) error {
	brokers, err := r.listClusterBrokers(ctx)
	if err != nil {
		return fmt.Errorf("listing cluster Broker CRs: %w", err)
	}

	now := time.Now()
	activeGrants := 0
	decommissionInFlight := false
	var candidates []*redpandav1alpha2.Broker

	for i := range brokers {
		b := &brokers[i]

		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				decommissionInFlight = true
			}
			continue
		}

		pod, err := r.getBrokerPod(ctx, b)
		if err != nil {
			return err
		}

		// Roll completion additionally requires a VERIFIED registration
		// (BrokerRegistered condition, recomputed by the Broker controller
		// from a live admin-API observation each reconcile): the rotated pod
		// must have rejoined under the same node_id (RFC rolling step 3).
		desiredChecksum := b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation]
		rollComplete := pod != nil &&
			!b.PodOutdated(pod) &&
			utils.IsPodReady(pod) &&
			apimeta.IsStatusConditionTrue(b.Status.Conditions, "BrokerRegistered")

		if grant := b.Annotations[feature.RollGrant.Key]; grant != "" {
			grantChecksum, deadline, ok := feature.ParseRollGrant(grant)
			switch {
			case !ok || grantChecksum != desiredChecksum:
				l.Info("revoking stale roll-grant", "broker", b.Name, "grant", grant)
				if err := r.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case rollComplete:
				l.Info("roll complete, revoking roll-grant", "broker", b.Name)
				if err := r.revokeRollGrant(ctx, b); err != nil {
					return err
				}
			case now.Before(deadline):
				activeGrants++
			default:
				// Expired grant on an unfinished roll: treated as released.
				// The broker sorts first among candidates and is re-granted
				// below, after a fresh health check.
				candidates = append(candidates, b)
			}
			continue
		}

		// Stuck brokers are roll candidates only when the pod is
		// unschedulable (the PV-affinity remediation case, where deleting
		// pod+PVC helps). Other Stuck classes — crash loops, image pull
		// failures, identity conflicts — would not be fixed by a rotation
		// and need an operator, not a grant.
		stuckUnschedulable := b.Status.Phase == redpandav1alpha2.BrokerPhaseStuck &&
			pod != nil && podUnschedulable(pod)
		needsRoll := (pod != nil && b.PodOutdated(pod)) || stuckUnschedulable
		if needsRoll {
			candidates = append(candidates, b)
		}
	}

	if activeGrants > 0 {
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "roll in flight, waiting for granted Broker to finish",
		}
	}
	if len(candidates) == 0 {
		// Quiescent: no roll outstanding and no grant active. If a
		// restart-requiring config change was being rolled out, it has now
		// reached every pod — clear Restarting (broker-mode counterpart of
		// the StatefulSet rolling-update path).
		if r.pandaCluster.Status.IsRestarting() {
			if err := r.stsResource.updateRestartingStatus(ctx, false); err != nil {
				return fmt.Errorf("clearing restarting status: %w", err)
			}
		}
		return nil
	}
	if decommissionInFlight {
		// Decommission progress is observed via Broker status updates, which
		// re-enqueue the Cluster.
		l.Info("holding roll-grants while a decommission is in flight")
		return nil
	}

	// Expired-grant holders (mid-roll) first, then node pool + index for a
	// deterministic order.
	sort.Slice(candidates, func(i, j int) bool {
		gi := candidates[i].Annotations[feature.RollGrant.Key] != ""
		gj := candidates[j].Annotations[feature.RollGrant.Key] != ""
		if gi != gj {
			return gi
		}
		pi, pj := candidates[i].Labels[labels.NodePoolKey], candidates[j].Labels[labels.NodePoolKey]
		if pi != pj {
			return pi < pj
		}
		return ptr.Deref(candidates[i].Spec.NetworkIndex, 0) < ptr.Deref(candidates[j].Spec.NetworkIndex, 0)
	})

	// RFC step 1: confirm cluster health before granting. Returns a
	// RequeueAfterError when unhealthy.
	if err := r.stsResource.isClusterHealthy(ctx); err != nil {
		return err
	}

	granted := candidates[0]
	checksum := granted.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerConfigChecksumAnnotation]
	grant := feature.FormatRollGrant(checksum, now.Add(feature.RollGrantTTL))
	l.Info("granting roll", "broker", granted.Name, "grant", grant, "outstanding", len(candidates))

	p := k8sclient.MergeFrom(granted.DeepCopy())
	if granted.Annotations == nil {
		granted.Annotations = map[string]string{}
	}
	granted.Annotations[feature.RollGrant.Key] = grant
	if err := r.Patch(ctx, granted, p); err != nil {
		return fmt.Errorf("granting roll to Broker %s: %w", granted.Name, err)
	}

	return &RequeueAfterError{
		RequeueAfter: RequeueDuration,
		Msg:          fmt.Sprintf("granted roll to Broker %s", granted.Name),
	}
}

func (r *BrokerSetResource) revokeRollGrant(ctx context.Context, b *redpandav1alpha2.Broker) error {
	p := k8sclient.MergeFrom(b.DeepCopy())
	delete(b.Annotations, feature.RollGrant.Key)
	if err := r.Patch(ctx, b, p); err != nil {
		return fmt.Errorf("revoking roll-grant on Broker %s: %w", b.Name, err)
	}
	return nil
}

func (r *BrokerSetResource) getBrokerPod(ctx context.Context, b *redpandav1alpha2.Broker) (*corev1.Pod, error) {
	var pod corev1.Pod
	err := r.Get(ctx, types.NamespacedName{Name: b.PodName(), Namespace: b.Namespace}, &pod)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting pod for Broker %s: %w", b.Name, err)
	}
	return &pod, nil
}

// listClusterBrokers lists Broker CRs owned by this cluster across ALL node
// pools — roll serialization is cluster-wide, not per-pool.
func (r *BrokerSetResource) listClusterBrokers(ctx context.Context) ([]redpandav1alpha2.Broker, error) {
	var list redpandav1alpha2.BrokerList
	if err := r.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: labels.ForCluster(r.pandaCluster).AsClientSelector(),
		Namespace:     r.pandaCluster.Namespace,
	}); err != nil {
		return nil, err
	}
	var owned []redpandav1alpha2.Broker
	for _, b := range list.Items {
		if metav1.IsControlledBy(&b, r.pandaCluster) {
			owned = append(owned, b)
		}
	}
	return owned, nil
}

// GetNodePool returns the node pool this BrokerSet manages.
func (r *BrokerSetResource) GetNodePool() *vectorizedv1alpha1.NodePoolSpecWithDeleted {
	return &r.nodePool
}

// --- Migration helpers ---

func (r *BrokerSetResource) getExistingStatefulSet(ctx context.Context) (*appsv1.StatefulSet, error) {
	stsKey := r.stsResource.Key()
	var sts appsv1.StatefulSet
	err := r.Get(ctx, stsKey, &sts)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sts, nil
}

func (r *BrokerSetResource) ensureBackupConfigMap(ctx context.Context, l logr.Logger, sts *appsv1.StatefulSet) error {
	cmName := fmt.Sprintf("%s-migration-backup", r.pandaCluster.Name)
	poolKey := fmt.Sprintf("%s.json", r.nodePool.Name)

	var cm corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: r.pandaCluster.Namespace}, &cm)
	if err == nil {
		if _, ok := cm.Data[poolKey]; ok {
			return nil
		}
		data, err := json.Marshal(sts)
		if err != nil {
			return err
		}
		cm.Data[poolKey] = string(data)
		return r.Update(ctx, &cm)
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	data, err := json.Marshal(sts)
	if err != nil {
		return err
	}

	cm = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: r.pandaCluster.Namespace,
			Labels: map[string]string{
				"redpanda.com/migration": "statefulset-to-broker",
			},
		},
		Data: map[string]string{
			poolKey: string(data),
		},
	}
	if err := controllerutil.SetControllerReference(r.pandaCluster, &cm, r.scheme); err != nil {
		return err
	}
	l.Info("migration: created STS backup ConfigMap", "name", cmName, "pool", r.nodePool.Name)
	return r.Create(ctx, &cm)
}

func verifyPVCRetention(sts *appsv1.StatefulSet) error {
	p := sts.Spec.PersistentVolumeClaimRetentionPolicy
	if p == nil {
		return nil // default is Retain/Retain
	}
	if p.WhenDeleted == appsv1.DeletePersistentVolumeClaimRetentionPolicyType ||
		p.WhenScaled == appsv1.DeletePersistentVolumeClaimRetentionPolicyType {
		return fmt.Errorf("StatefulSet %s has PVC retention policy with Delete; refusing migration to avoid data loss", sts.Name)
	}
	return nil
}

func removeOwnerRef(obj k8sclient.Object, uid types.UID) bool {
	refs := obj.GetOwnerReferences()
	for i, ref := range refs {
		if ref.UID == uid {
			obj.SetOwnerReferences(append(refs[:i], refs[i+1:]...))
			return true
		}
	}
	return false
}

func indexBrokers(brokers []redpandav1alpha2.Broker) map[int32]*redpandav1alpha2.Broker {
	m := map[int32]*redpandav1alpha2.Broker{}
	for i := range brokers {
		if brokers[i].Spec.NetworkIndex != nil {
			m[*brokers[i].Spec.NetworkIndex] = &brokers[i]
		}
	}
	return m
}

// setMigrationCondition records STS→Broker migration progress on the Cluster
// status so operators can observe it (the migration state itself is derived
// from world state, never persisted). Writes only when the condition value
// actually changes; conflicts with other status writers are retried.
func setMigrationCondition(ctx context.Context, c k8sclient.Client, cluster *vectorizedv1alpha1.Cluster, l logr.Logger, status corev1.ConditionStatus, reason, message string) {
	if cond := cluster.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType); cond != nil &&
		cond.Status == status && cond.Reason == reason && cond.Message == message {
		return
	}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var fresh vectorizedv1alpha1.Cluster
		if err := c.Get(ctx, k8sclient.ObjectKeyFromObject(cluster), &fresh); err != nil {
			return err
		}
		if !fresh.Status.SetCondition(vectorizedv1alpha1.BrokerMigrationConditionType, status, reason, message) {
			cluster.Status.Conditions = fresh.Status.Conditions
			return nil
		}
		if err := c.Status().Update(ctx, &fresh); err != nil {
			return err
		}
		cluster.Status.Conditions = fresh.Status.Conditions
		return nil
	})
	if err != nil {
		// Best-effort observability — never fail the migration over it.
		l.Error(err, "failed to update BrokerMigration condition", "reason", reason)
	}
}

// MarkBrokersForRestart records a restart-requiring cluster-config version in
// each Broker's desired pod template (the `kubectl rollout restart` pattern):
// pods inherit the annotation at creation, so a live pod whose value differs
// from the desired template needs a rotation. The resulting restarts ride the
// roll-grant machinery one broker at a time, and Status.Restarting clears
// once no pod drifts. Only Broker SPECS are written — the cluster controller
// never touches pods, which belong to the Broker controller.
func MarkBrokersForRestart(ctx context.Context, c k8sclient.Client, cluster *vectorizedv1alpha1.Cluster, configVersion int64) error {
	var list redpandav1alpha2.BrokerList
	if err := c.List(ctx, &list, &k8sclient.ListOptions{
		LabelSelector: labels.ForCluster(cluster).AsClientSelector(),
		Namespace:     cluster.Namespace,
	}); err != nil {
		return fmt.Errorf("listing Broker CRs: %w", err)
	}

	version := fmt.Sprintf("%d", configVersion)
	for i := range list.Items {
		b := &list.Items[i]
		if !metav1.IsControlledBy(b, cluster) {
			continue
		}
		if b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation] == version {
			continue
		}
		patch := k8sclient.MergeFrom(b.DeepCopy())
		if b.Spec.PodTemplate.Annotations == nil {
			b.Spec.PodTemplate.Annotations = map[string]string{}
		}
		b.Spec.PodTemplate.Annotations[redpandav1alpha2.BrokerClusterConfigVersionAnnotation] = version
		if err := c.Patch(ctx, b, patch); err != nil {
			return fmt.Errorf("marking Broker %s for restart: %w", b.Name, err)
		}
	}
	return nil
}

// podUnschedulable reports whether the pod cannot be scheduled — the Stuck
// class that a granted pod+PVC recreation (PV-affinity remediation) can fix.
func podUnschedulable(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
			return true
		}
	}
	return false
}

// verifyRollbackPreconditions blocks rollback while a rotation or
// decommission is mid-flight: every non-decommissioned Broker must have its
// pod present and no Broker may hold an unexpired roll-grant. Fully
// Decommissioned brokers (scale-down leftovers) are inert and do not block.
func verifyRollbackPreconditions(ctx context.Context, c k8sclient.Client, l logr.Logger, brokers []redpandav1alpha2.Broker) error {
	block := func(reason string) error {
		l.Info("rollback blocked, waiting for in-flight operations to finish", "reason", reason)
		return &RequeueAfterError{
			RequeueAfter: RequeueDuration,
			Msg:          "rollback blocked: " + reason,
		}
	}

	now := time.Now()
	for i := range brokers {
		b := &brokers[i]
		if b.Spec.Decommission {
			if b.Status.Phase != redpandav1alpha2.BrokerPhaseDecommissioned {
				return block(fmt.Sprintf("Broker %s is decommissioning", b.Name))
			}
			continue
		}
		if grant := b.Annotations[feature.RollGrant.Key]; grant != "" {
			if _, deadline, ok := feature.ParseRollGrant(grant); ok && now.Before(deadline) {
				return block(fmt.Sprintf("Broker %s holds an active roll-grant", b.Name))
			}
		}
		var pod corev1.Pod
		if err := c.Get(ctx, types.NamespacedName{Name: b.PodName(), Namespace: b.Namespace}, &pod); err != nil {
			if apierrors.IsNotFound(err) {
				return block(fmt.Sprintf("pod %s is missing (rotation in flight)", b.PodName()))
			}
			return err
		}
	}
	return nil
}

// restoreStatefulSetsFromBackup recreates the pre-migration StatefulSets from
// the migration backup ConfigMap (RFC Q1: the backup is the one piece of
// state not derivable). Re-rendering could differ from what the pods were
// built from — e.g. after an operator upgrade mid-migration — and an STS that
// doesn't match its re-adopted pods would immediately roll them. Restoring
// the exact backup re-adopts without disruption; any genuine drift then rolls
// through the normal health-gated StatefulSet update flow.
//
// Returns false when there is no backup to restore from — the cluster
// controller then falls back to re-rendering the StatefulSet.
func restoreStatefulSetsFromBackup(ctx context.Context, c k8sclient.Client, scheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, l logr.Logger) (bool, error) {
	cmName := fmt.Sprintf("%s-migration-backup", cluster.Name)
	var cm corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cluster.Namespace}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("rollback: no migration backup ConfigMap, StatefulSets will be re-rendered", "name", cmName)
			return false, nil
		}
		return false, err
	}

	for key, data := range cm.Data {
		var backup appsv1.StatefulSet
		if err := json.Unmarshal([]byte(data), &backup); err != nil {
			return false, fmt.Errorf("unmarshaling StatefulSet backup %s/%s: %w", cmName, key, err)
		}
		restored := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:        backup.Name,
				Namespace:   cluster.Namespace,
				Labels:      backup.Labels,
				Annotations: backup.Annotations,
			},
			Spec: backup.Spec,
		}
		if err := controllerutil.SetControllerReference(cluster, restored, scheme); err != nil {
			return false, fmt.Errorf("setting owner reference on restored StatefulSet %s: %w", restored.Name, err)
		}
		l.Info("rollback: restoring StatefulSet from migration backup", "sts", restored.Name, "key", key)
		if err := c.Create(ctx, restored); err != nil && !apierrors.IsAlreadyExists(err) {
			return false, fmt.Errorf("restoring StatefulSet %s: %w", restored.Name, err)
		}
	}
	return true, nil
}

// RollbackBrokerCRs cleans up Broker CRs when the migration annotation is
// removed, allowing the StatefulSet to re-adopt pods.
func RollbackBrokerCRs(ctx context.Context, c k8sclient.Client, scheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, l logr.Logger) error {
	clusterLabels := labels.ForCluster(cluster)
	var brokerList redpandav1alpha2.BrokerList
	if err := c.List(ctx, &brokerList, &k8sclient.ListOptions{
		LabelSelector: clusterLabels.AsClientSelector(),
		Namespace:     cluster.Namespace,
	}); err != nil {
		return err
	}

	if len(brokerList.Items) == 0 {
		return nil
	}

	// Rollback is gated on LOCAL state only — never on admin-API health.
	// It is the escape hatch and must stay available on a degraded cluster;
	// it is blocked only while another disruptive operation is mid-flight.
	if err := verifyRollbackPreconditions(ctx, c, l, brokerList.Items); err != nil {
		var requeueErr *RequeueAfterError
		if stderrors.As(err, &requeueErr) {
			setMigrationCondition(ctx, c, cluster, l, corev1.ConditionFalse,
				vectorizedv1alpha1.BrokerMigrationReasonBlocked, requeueErr.Msg)
		}
		return err
	}

	l.Info("rollback: cleaning up Broker CRs", "count", len(brokerList.Items))

	// Strip Broker CR ownerRefs from pods so the STS can re-adopt.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		podName := b.PodName()
		var pod corev1.Pod
		if err := c.Get(ctx, types.NamespacedName{Name: podName, Namespace: cluster.Namespace}, &pod); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if removeOwnerRef(&pod, b.UID) {
			l.Info("rollback: stripping Broker ownerRef from pod", "pod", podName)
			if err := c.Update(ctx, &pod); err != nil {
				return fmt.Errorf("stripping Broker ownerRef from pod %s: %w", podName, err)
			}
		}
	}

	// Strip Broker CR ownerRefs from PVCs.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		for _, ec := range b.Spec.Storage.ExistingClaims {
			var pvc corev1.PersistentVolumeClaim
			if err := c.Get(ctx, types.NamespacedName{Name: ec.Name, Namespace: cluster.Namespace}, &pvc); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return err
			}
			if removeOwnerRef(&pvc, b.UID) {
				l.Info("rollback: stripping Broker ownerRef from PVC", "pvc", ec.Name)
				if err := c.Update(ctx, &pvc); err != nil {
					return fmt.Errorf("stripping Broker ownerRef from PVC %s: %w", ec.Name, err)
				}
			}
		}
	}

	// Remove finalizers and delete Broker CRs with orphan propagation.
	// Finalizer removal prevents the Broker controller's reconcileDelete from
	// running decommission. Orphan propagation prevents the GC from cascade-
	// deleting pods that the Broker controller re-adopted between our ownerRef
	// strip above and the delete here.
	for i := range brokerList.Items {
		b := &brokerList.Items[i]
		if controllerutil.RemoveFinalizer(b, "cluster.redpanda.com/broker-decommission") {
			if err := c.Update(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("removing finalizer from Broker CR %s: %w", b.Name, err)
			}
		}
		l.Info("rollback: deleting Broker CR", "name", b.Name)
		if err := c.Delete(ctx, b, k8sclient.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting Broker CR %s: %w", b.Name, err)
		}
	}

	// Recreate the StatefulSets from the migration backup, then drop the
	// backup — only after a successful restore, so a failed rollback pass
	// never loses the one non-derivable piece of state.
	restored, err := restoreStatefulSetsFromBackup(ctx, c, scheme, cluster, l)
	if err != nil {
		return err
	}
	if restored {
		cmName := fmt.Sprintf("%s-migration-backup", cluster.Name)
		var cm corev1.ConfigMap
		if err := c.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cluster.Namespace}, &cm); err == nil {
			l.Info("rollback: deleting migration backup ConfigMap", "name", cmName)
			if err := c.Delete(ctx, &cm); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	setMigrationCondition(ctx, c, cluster, l, corev1.ConditionTrue,
		vectorizedv1alpha1.BrokerMigrationReasonRolledBack, "Broker CRs removed; StatefulSet manages all pods")

	return nil
}
