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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
// StatefulSetResource when the cluster has the use-broker-cr
// annotation. It is a thin V1 (vectorized Cluster) adapter around the
// CR-agnostic machinery in operator/internal/brokerset.
type BrokerSetResource struct {
	k8sclient.Client
	scheme       *runtime.Scheme
	pandaCluster *vectorizedv1alpha1.Cluster
	// stsResource renders the canonical pod template (obj()) that we convert
	// into Broker CRs, and — while a live StatefulSet still exists
	// (migration not yet handed over) — keeps converging it via Ensure. It
	// never CREATES a StatefulSet: Ensure is only invoked against one that
	// already exists.
	stsResource *StatefulSetResource
	nodePool    vectorizedv1alpha1.NodePoolSpecWithDeleted
	// reporter accumulates this pool's migration progress. Migration runs
	// per pool but the BrokerMigration condition is cluster-scoped, so pools
	// must never write it directly — the cluster controller flushes one
	// aggregate after all pools have reconciled.
	reporter brokerset.MigrationReporter
	// arbitration shares the reconcile pass's disruptive-write state across
	// all pools' BrokerSets (one per reconcile, owned by the cluster
	// controller) so the one-disruptive-operation-at-a-time gates see this
	// pass's own writes, which the informer cache cannot.
	arbitration *brokerset.Arbitration
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
	schemaRegistryClientFactory SchemaRegistryClientsFactory,
	dialer redpanda.DialContextFunc,
	decommissionWaitInterval time.Duration,
	logger logr.Logger,
	metricsTimeout time.Duration,
	nodePool vectorizedv1alpha1.NodePoolSpecWithDeleted,
	autoDeletePVCs bool,
	brokerPodNodeUnavailableToleration time.Duration,
	reporter brokerset.MigrationReporter,
	arbitration *brokerset.Arbitration,
) *BrokerSetResource {
	sts := NewStatefulSet(
		client, pandaCluster, scheme,
		serviceFQDN, serviceName, nodePortName,
		volumeProvider, adminTLSConfigProvider,
		serviceAccountName, configuratorSettings, cfg,
		adminAPIClientFactory, schemaRegistryClientFactory, dialer, decommissionWaitInterval,
		logger, metricsTimeout, nodePool, autoDeletePVCs,
		brokerPodNodeUnavailableToleration,
	)
	return &BrokerSetResource{
		Client:       client,
		scheme:       scheme,
		pandaCluster: pandaCluster,
		stsResource:  sts,
		nodePool:     nodePool,
		reporter:     reporter,
		arbitration:  arbitration,
		logger:       logger.WithName("BrokerSetResource"),
	}
}

// Key exists solely to satisfy the [Resource] interface.
// Don't use it.
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
			Group: ptr.To(vectorizedv1alpha1.GroupName),
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
			// (if one was pending) has reached every pod. Clear the CLUSTER-
			// level Restarting flag directly: broker mode never sets the
			// per-pool flag (that is STS-mode runUpdate's doing, and broker-
			// mode reportStatus rebuilds pool statuses from scratch, erasing
			// it anyway), so updateRestartingStatus's per-pool change guard
			// would no-op forever — leaving Restarting stuck true and every
			// future restart-requiring config change gated off.
			if !r.pandaCluster.Status.IsRestarting() {
				return nil
			}
			r.pandaCluster.Status.SetRestarting(false)
			if err := r.Status().Update(ctx, r.pandaCluster); err != nil {
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
		Reporter:    r.reporter,
		Arbitration: r.arbitration,
		Logger:      l,
	}
}

func (r *BrokerSetResource) Ensure(ctx context.Context) error {
	l := r.logger.WithValues("nodepool", r.nodePool.Name)

	// While the live StatefulSet still exists (migration not yet handed
	// over), it remains the authoritative pod manager: keep converging it —
	// template updates, health-gated rolls, scaling — exactly as in
	// StatefulSet mode. Without this, a spec or config change made after the
	// migrate annotation was set (including an operator upgrade that renders
	// a different template) could never reach the pods, and the migration
	// preconditions ("config change pending, StatefulSet not yet updated")
	// would block forever. Guarded on existence so a broker-born cluster or
	// a completed migration never (re)creates the StatefulSet.
	var live appsv1.StatefulSet
	getErr := r.Get(ctx, r.stsResource.Key(), &live)
	if getErr != nil && !apierrors.IsNotFound(getErr) {
		return fmt.Errorf("checking for existing StatefulSet: %w", getErr)
	}
	if getErr == nil && live.DeletionTimestamp.IsZero() {
		if err := r.stsResource.Ensure(ctx); err != nil {
			return err
		}
	} else if r.nodePool.Deleted {
		// A deleted pool whose StatefulSet is mid-termination is neither
		// STS-managed (branch above requires a live STS) nor drainable yet:
		// the engine's Ensure would find the still-listed StatefulSet and
		// enter the migration state machine with no desired spec to feed it.
		// Wait the termination out; the next pass takes the drain path.
		if getErr == nil {
			return &RequeueAfterError{
				RequeueAfter: RequeueDuration,
				Msg:          fmt.Sprintf("waiting for StatefulSet %s of deleted pool to finish terminating", live.Name),
			}
		}
		// A deleted pool with no StatefulSet is broker-backed: its spec may
		// be a minimal reconstruction from Broker CR labels (see
		// nodepools.GetNodePoolsWithBrokerBacked), so nothing can be rendered
		// from it. A nil desired StatefulSet is exactly the engine's drain
		// path — no desired brokers, excess brokers decommissioned one at a
		// time. Deleted pools whose StatefulSet still exists (removed
		// mid-migration) keep the branch above: the live StatefulSet remains
		// the pod manager until the migration hands over.
		return r.core(l).Ensure(ctx, r.stsResource.Key(), nil, 0)
	}

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
	_, err := brokerset.Rollback(ctx, brokerset.RollbackConfig{
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
	return err
}

// NewMigrationPoolReporter returns the migration reporter for one node pool:
// reports accumulate into agg (the cluster controller flushes one aggregate
// after all pools reconcile), while the ShouldReport* predicates keep
// reading the Cluster's BrokerMigration condition.
func NewMigrationPoolReporter(agg *brokerset.MigrationAggregator, pool string, c k8sclient.Client, cluster *vectorizedv1alpha1.Cluster, l logr.Logger) brokerset.MigrationReporter {
	return agg.PoolReporter(pool, &v1MigrationReporter{client: c, cluster: cluster, logger: l})
}

// FlushBrokerMigrationCondition writes the aggregate of every pool's
// migration report to the Cluster's BrokerMigration condition. Migration
// runs per pool but the condition is cluster-scoped: aggregating before the
// single write is what keeps a finished pool from declaring Complete while
// another is still blocked. Call after all pools have reconciled; empty or
// partial information (a pool errored before reporting) skips the write.
func FlushBrokerMigrationCondition(ctx context.Context, c k8sclient.Client, cluster *vectorizedv1alpha1.Cluster, l logr.Logger, agg *brokerset.MigrationAggregator) {
	status, reason, message, ok := agg.Aggregate()
	if !ok {
		return
	}
	setMigrationCondition(ctx, c, cluster, l, status, reason, message)
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

// shouldReport reports whether migration progress was previously recorded
// on the Cluster's BrokerMigration condition and the condition has not yet
// reached the given terminal reason. Clusters that never migrated have no
// condition and must never gain one from a steady-state promotion.
func (rep *v1MigrationReporter) shouldReport(terminalReason string) bool {
	cond := rep.cluster.Status.GetCondition(vectorizedv1alpha1.BrokerMigrationConditionType)
	return cond != nil && cond.Reason != terminalReason
}

func (rep *v1MigrationReporter) ShouldReportComplete(context.Context) bool {
	return rep.shouldReport(brokerset.MigrationReasonComplete)
}

func (rep *v1MigrationReporter) ShouldReportRolledBack(context.Context) bool {
	return rep.shouldReport(brokerset.MigrationReasonRolledBack)
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
