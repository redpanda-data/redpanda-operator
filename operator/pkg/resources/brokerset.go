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
	"fmt"
	"maps"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/client"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/brokerset"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	resourcetypes "github.com/redpanda-data/redpanda-operator/operator/pkg/resources/types"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

var _ Resource = &BrokerSetResource{}

// BrokerSetResource manages Broker CRs for a single node pool, replacing
// StatefulSetResource when the cluster has the migrate-to-broker-cr
// annotation. It is a thin V1 (vectorized Cluster) adapter around the
// CR-agnostic machinery in operator/internal/brokerset.
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

// core assembles the CR-agnostic brokerset engine with the V1-specific
// pieces: labels, ClusterRef, the V1 checksum annotation, and callbacks
// bridging into the Cluster's status and the STS resource's admin health
// check.
func (r *BrokerSetResource) core(l logr.Logger) *brokerset.BrokerSet {
	clusterLabels := labels.ForCluster(r.pandaCluster)
	poolLabels := clusterLabels.WithNodePool(r.nodePool.Name)

	brokerLabels := maps.Clone(map[string]string(poolLabels))
	// ClusterNameLabel must be set on every Broker CR: Broker.PodName()
	// relies on it to recover the cluster half of the pod name when the
	// ClusterRef names a NodePool rather than the cluster.
	brokerLabels[redpandav1alpha2.ClusterNameLabel] = r.pandaCluster.Name

	return &brokerset.BrokerSet{
		Client: r.Client,
		Scheme: r.scheme,
		Owner:  r.pandaCluster,
		ClusterRef: redpandav1alpha2.ClusterRef{
			Group: ptr.To("redpanda.vectorized.io"),
			Kind:  ptr.To("Cluster"),
			Name:  r.pandaCluster.Name,
		},
		PoolName:          r.nodePool.Name,
		BrokerLabels:      brokerLabels,
		PoolSelector:      poolLabels.AsClientSelectorForNodePool(),
		ClusterSelector:   clusterLabels.AsClientSelector(),
		PodSelector:       poolLabels.AsClientSelectorForNodePool(),
		ConfigChecksumKey: ConfigMapHashAnnotationKey,
		IsClusterHealthy:  r.stsResource.isClusterHealthy,
		OnQuiesced: func(ctx context.Context) error {
			// A completed roll-out means a restart-requiring config change
			// (if one was pending) has reached every pod.
			if !r.pandaCluster.Status.IsRestarting() {
				return nil
			}
			if err := r.stsResource.updateRestartingStatus(ctx, false); err != nil {
				return fmt.Errorf("clearing restarting status: %w", err)
			}
			return nil
		},
		MigrationBlockedReason: func(context.Context) (string, error) {
			if r.pandaCluster.Status.IsRestarting() {
				return "cluster is restarting", nil
			}
			if r.pandaCluster.Status.DecommissioningNode != nil {
				return fmt.Sprintf("decommission of node_id=%d in progress", *r.pandaCluster.Status.DecommissioningNode), nil
			}
			return "", nil
		},
		Reporter: &v1MigrationReporter{
			client:  r.Client,
			cluster: r.pandaCluster,
			logger:  l,
		},
		Logger: l,
	}
}

func (r *BrokerSetResource) Ensure(ctx context.Context) error {
	l := r.logger.WithValues("nodepool", r.nodePool.Name)

	// Render the desired STS (obj()), never the live one. This ensures the
	// configmap checksum matches what steady state will compute
	// post-migration, preventing spurious pod rotation during adoption.
	var desired *appsv1.StatefulSet
	stsObj, err := r.stsResource.obj(ctx)
	if err != nil {
		return fmt.Errorf("rendering desired StatefulSet: %w", err)
	}
	if stsObj != nil {
		desired = stsObj.(*appsv1.StatefulSet)
	}

	return r.core(l).Ensure(ctx, r.stsResource.Key(), desired, ptr.Deref(r.nodePool.Replicas, 0))
}

// GetNodePool returns the node pool this BrokerSet manages.
func (r *BrokerSetResource) GetNodePool() *vectorizedv1alpha1.NodePoolSpecWithDeleted {
	return &r.nodePool
}

// --- thin delegates (also the unit-test surface) ---

func (r *BrokerSetResource) ensureRollGrants(ctx context.Context, l logr.Logger) error {
	return r.core(l).EnsureRollGrants(ctx, l)
}

func (r *BrokerSetResource) verifyMigrationPreconditions(ctx context.Context, l logr.Logger, liveSTS, desiredSTS *appsv1.StatefulSet) error {
	return r.core(l).VerifyMigrationPreconditions(ctx, l, liveSTS, desiredSTS)
}

func (r *BrokerSetResource) ensureDesiredBroker(ctx context.Context, l logr.Logger, existingBroker, d *redpandav1alpha2.Broker) error {
	return r.core(l).EnsureDesiredBroker(ctx, l, existingBroker, d)
}

//nolint:unparam // test-only shim; production callers go through brokerset directly
func (r *BrokerSetResource) reconcileExcessBrokers(ctx context.Context, l logr.Logger, existingByIndex map[int32]*redpandav1alpha2.Broker, desiredReplicas int32, decommissionInFlight bool) error {
	return r.core(l).ReconcileExcessBrokers(ctx, l, existingByIndex, desiredReplicas, decommissionInFlight)
}

func (r *BrokerSetResource) updateBroker(ctx context.Context, l logr.Logger, existing, desired *redpandav1alpha2.Broker) error {
	return r.core(l).UpdateBroker(ctx, l, existing, desired)
}

// brokersFromStatefulSet converts a StatefulSet spec into Broker CRs, one per
// ordinal — the V1-shaped entry point to brokerset.RenderBrokers.
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

	brokerLabels := maps.Clone(map[string]string(labels.ForCluster(cluster).WithNodePool(nodePool.Name)))
	brokerLabels[redpandav1alpha2.ClusterNameLabel] = cluster.Name

	set := &brokerset.BrokerSet{
		Scheme: scheme,
		Owner:  cluster,
		ClusterRef: redpandav1alpha2.ClusterRef{
			Group: ptr.To("redpanda.vectorized.io"),
			Kind:  ptr.To("Cluster"),
			Name:  cluster.Name,
		},
		PoolName:          nodePool.Name,
		BrokerLabels:      brokerLabels,
		ConfigChecksumKey: ConfigMapHashAnnotationKey,
	}
	return set.RenderBrokers(sts, replicas, migration)
}

func indexBrokers(brokers []redpandav1alpha2.Broker) map[int32]*redpandav1alpha2.Broker {
	return brokerset.IndexBrokers(brokers)
}

func verifyRollbackPreconditions(ctx context.Context, c k8sclient.Client, l logr.Logger, brokers []redpandav1alpha2.Broker) error {
	return brokerset.VerifyRollbackPreconditions(ctx, c, l, brokers)
}

// MarkBrokersForRestart records a restart-requiring cluster-config version in
// each Broker's desired pod template (the `kubectl rollout restart` pattern):
// pods inherit the annotation at creation, so a live pod whose value differs
// from the desired template needs a rotation. The resulting restarts ride the
// roll-grant machinery one broker at a time, and Status.Restarting clears
// once no pod drifts. Only Broker SPECS are written — the cluster controller
// never touches pods, which belong to the Broker controller.
func MarkBrokersForRestart(ctx context.Context, c k8sclient.Client, cluster *vectorizedv1alpha1.Cluster, configVersion int64) error {
	return brokerset.MarkForRestart(ctx, c, cluster,
		labels.ForCluster(cluster).AsClientSelector(), fmt.Sprintf("%d", configVersion))
}

// RollbackBrokerCRs cleans up Broker CRs when the migration annotation is
// removed, allowing the StatefulSet to re-adopt pods.
func RollbackBrokerCRs(ctx context.Context, c k8sclient.Client, scheme *runtime.Scheme, cluster *vectorizedv1alpha1.Cluster, l logr.Logger) error {
	return brokerset.Rollback(ctx, brokerset.RollbackConfig{
		Client:          c,
		Scheme:          scheme,
		Owner:           cluster,
		ClusterSelector: labels.ForCluster(cluster).AsClientSelector(),
		Reporter: &v1MigrationReporter{
			client:  c,
			cluster: cluster,
			logger:  l,
		},
		Logger: l,
	})
}

// v1MigrationReporter records STS→Broker migration progress on the V1
// Cluster's BrokerMigration status condition.
type v1MigrationReporter struct {
	client  k8sclient.Client
	cluster *vectorizedv1alpha1.Cluster
	logger  logr.Logger
}

var _ brokerset.MigrationReporter = (*v1MigrationReporter)(nil)

func (rep *v1MigrationReporter) Report(ctx context.Context, status corev1.ConditionStatus, reason, message string) {
	setMigrationCondition(ctx, rep.client, rep.cluster, rep.logger, status, reason, message)
}

func (rep *v1MigrationReporter) NeedsCompletion(context.Context) bool {
	cond := rep.cluster.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
	return cond != nil && cond.Reason != vectorizedv1alpha1.BrokerMigrationReasonComplete
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
