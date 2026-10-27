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

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/finalizers,verbs=update

type MulticlusterReconciler struct {
	manager multicluster.Manager
}

func (r *MulticlusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithValues("cluster", req.ClusterName, "namespace", req.Namespace, "name", req.Name)
	logger.Info("reconciling cluster")

	cluster, err := r.manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		// we encountered an error fetching the cluster, just ignore the reconciliation request
		return ctrl.Result{}, nil
	}
	k8sClient := cluster.GetClient()

	stretchCluster := &redpandav1alpha2.StretchCluster{}
	if err := k8sClient.Get(ctx, req.NamespacedName, stretchCluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !stretchCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(stretchCluster, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, stretchCluster) {
		if err := k8sClient.Update(ctx, stretchCluster); err != nil {
			logger.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	// do whatever reconciliation here

	return ctrl.Result{}, nil
}

<<<<<<< HEAD
func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager) error {
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(controller.TypedOptions[mcreconcile.Request]{
=======
// findExistingBootstrapPassword scans reachable clusters for a Secret named
// `name` carrying a non-empty password under `key`, returning the first one found
// and the cluster it came from (canonicalised for display), or ("", "") if none.
// Used during secretKeyRef migration to locate the legacy operator-managed
// bootstrap password a pre-existing cluster was actually bootstrapped with. Scan
// order follows GetClusterNames, mirroring the Phase-1 reachability/timeout
// handling so a partitioned peer cannot stall the reconcile.
func (r *MulticlusterReconciler) findExistingBootstrapPassword(ctx context.Context, sc *redpandav1alpha2.StretchCluster, clusterNames []string, name, key string) (password, clusterCanonical string) {
	logger := log.FromContext(ctx)
	for _, clusterName := range clusterNames {
		if clusterName != mcmanager.LocalCluster && !r.Manager.IsClusterReachable(clusterName) {
			continue
		}
		cl, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			continue
		}
		var existing corev1.Secret
		getCtx, getCancel := context.WithTimeout(ctx, lifecycle.CallTimeoutFor(clusterName))
		err = cl.GetClient().Get(getCtx, client.ObjectKey{Namespace: sc.Namespace, Name: name}, &existing)
		getCancel()
		if err != nil {
			continue
		}
		if pw, ok := existing.Data[key]; ok && len(pw) > 0 {
			logger.V(log.TraceLevel).Info("found legacy bootstrap user secret for migration", "cluster", clusterName, "secret", name)
			return string(pw), lifecycle.CanonicalClusterName(clusterName, r.Manager.GetLocalClusterName)
		}
	}
	return "", ""
}

// syncCA ensures that a shared root CA Secret exists for each operator-managed
// TLS cert. The CA is generated once (on any cluster) and then synced to every
// known cluster, similar to how syncBootstrapUser works for SASL credentials.
func (r *MulticlusterReconciler) syncCA(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "syncCA")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error syncing CA")
		}
		trace.EndSpan(span, err)
	}()

	sc := state.cluster.StretchCluster

	// Apply defaults to a copy of the spec so BootstrappedCertNames sees the
	// same TLS configuration that the renderer will use (MergeDefaults enables
	// TLS by default when TLS is nil).
	defaultedSpec := *sc.Spec.DeepCopy()
	defaultedSpec.MergeDefaults()
	managedCerts := rendermulticluster.BootstrappedCertNames(state.cluster.GetAllBrokerPools())
	if len(managedCerts) == 0 {
		logger.V(log.TraceLevel).Info("no operator-managed TLS certs, skipping CA sync")
		return ctrl.Result{}, nil
	}

	clusterNames := r.Manager.GetClusterNames()

	for _, certName := range managedCerts {
		secretName := rendermulticluster.CASecretName(sc.Name, certName)

		// Phase 1: scan all clusters for an existing CA secret.
		var canonicalSecret *corev1.Secret
		for _, clusterName := range clusterNames {
			cl, err := r.Manager.GetCluster(ctx, clusterName)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "getting cluster %s", clusterName)
			}

			var existing corev1.Secret
			if err := cl.GetClient().Get(ctx, client.ObjectKey{
				Namespace: sc.Namespace,
				Name:      secretName,
			}, &existing); err == nil {
				canonicalSecret = &existing
				logger.V(log.TraceLevel).Info("found existing CA secret", "cert", certName, "cluster", clusterName)
				break
			}
		}

		// Phase 2: if no secret exists anywhere, generate a new CA.
		if canonicalSecret == nil {
			ca, err := bootstrap.GenerateCA(caOrganization, secretName, nil)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "generating CA for cert %q", certName)
			}
			canonicalSecret = &corev1.Secret{
				Data: map[string][]byte{
					corev1.TLSCertKey:       ca.Bytes(),
					corev1.TLSPrivateKeyKey: ca.PrivateKeyBytes(),
					"ca.crt":                ca.Bytes(),
				},
			}
			logger.V(log.DebugLevel).Info("generated new CA", "cert", certName)
		}

		// Phase 3: ensure the CA secret exists in every cluster.
		for _, clusterName := range clusterNames {
			if !r.Manager.IsClusterReachable(clusterName) {
				logger.Info("cluster unreachable, skipping CA sync", "cluster", clusterName, "cert", certName)
				continue
			}

			cl, err := r.Manager.GetCluster(ctx, clusterName)
			if err != nil {
				logger.Info("cluster unreachable, skipping CA sync", "cluster", clusterName, "cert", certName, "error", err)
				continue
			}

			k8sClient := cl.GetClient()

			var existing corev1.Secret
			if err := k8sClient.Get(ctx, client.ObjectKey{
				Namespace: sc.Namespace,
				Name:      secretName,
			}, &existing); err == nil {
				// Secret already exists — don't overwrite. CA secrets are
				// create-once: overwriting would invalidate all existing
				// leaf certs signed by the old CA.
				continue
			}

			// Fetch the StretchCluster from this cluster for owner reference.
			// Skip clusters where the SC doesn't exist or is being deleted.
			localSC := &redpandav1alpha2.StretchCluster{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), localSC); err != nil {
				if client.IgnoreNotFound(err) == nil {
					logger.V(log.TraceLevel).Info("StretchCluster not found on cluster, skipping CA sync", "cluster", clusterName)
					continue
				}
				logger.Info("could not fetch StretchCluster from cluster, skipping CA sync", "cluster", clusterName, "error", err)
				continue
			}
			if !localSC.DeletionTimestamp.IsZero() {
				logger.V(log.TraceLevel).Info("StretchCluster is being deleted on cluster, skipping CA sync", "cluster", clusterName)
				continue
			}

			caLabels := r.LifecycleClient.AddOwnerLabels(state.cluster)
			caLabels[lifecycle.GCLabel] = "false"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: sc.Namespace,
					Labels:    caLabels,
					OwnerReferences: []metav1.OwnerReference{
						*metav1.NewControllerRef(localSC, redpandav1alpha2.SchemeGroupVersion.WithKind("StretchCluster")),
					},
				},
				Type: corev1.SecretTypeTLS,
				Data: canonicalSecret.Data,
			}

			if err := k8sClient.Create(ctx, secret); err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "creating CA secret %q in cluster %s", secretName, clusterName)
			}
			logger.Info("created CA secret", "cert", certName, "cluster", clusterName, "secret", secretName)
		}
	}

	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcileResources(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcileResources")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error reconciling resources")
			if internalclient.IsTerminalClientError(err) {
				state.status.StretchClusterStatus.SetResourcesSynced(statuses.StretchClusterResourcesSyncedReasonTerminalError, err.Error())
			} else {
				state.status.StretchClusterStatus.SetResourcesSynced(statuses.StretchClusterResourcesSyncedReasonError, err.Error())
			}
		}
		// Success status for ResourcesSynced is set by reconcileDecommission
		// which runs later and has the full picture including health status.

		trace.EndSpan(span, err)
	}()

	if err = r.LifecycleClient.SyncAll(ctx, state.cluster); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcilePools(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcilePools")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error reconciling pools")
			if internalclient.IsTerminalClientError(err) {
				state.status.StretchClusterStatus.SetResourcesSynced(statuses.StretchClusterResourcesSyncedReasonTerminalError, err.Error())
			} else {
				state.status.StretchClusterStatus.SetResourcesSynced(statuses.StretchClusterResourcesSyncedReasonError, err.Error())
			}
		}
		// Success status for ResourcesSynced is set after both reconcileResources
		// and reconcilePools complete successfully, in the main reconcile loop.

		trace.EndSpan(span, err)
	}()

	if !state.pools.CheckScale(ctx) {
		// A scale/roll is underway — skip pool mutations this pass so we don't
		// stack changes mid-scale. Do NOT abort: returning a RequeueAfter here
		// would skip the Phase-3 cluster reconcilers (maintenance-mode clear,
		// stale-disk wipe, decommission), which must still run while a pod is churning
		// (e.g. a bad_rejoin pod being replaced flips len(pods)!=spec.Replicas).
		// The fast requeue for the still-scaling state is applied at the end of
		// Reconcile.
		logger.V(log.TraceLevel).Info("scale operation currently underway; skipping pool mutations this pass")
		return ctrl.Result{}, nil
	}

	logger.V(log.TraceLevel).Info("ready to scale and apply broker pools", "existing", state.pools.ExistingStatefulSets(), "desired", state.pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range state.pools.ToCreate() {
		logger.V(log.TraceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchPoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "creating statefulset")
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range state.pools.ToScaleUp() {
		logger.V(log.TraceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchPoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "scaling up statefulset")
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range state.pools.RequiresUpdate() {
		logger.V(log.TraceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.LifecycleClient.PatchPoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "updating statefulset")
		}
	}

	// NOTE: the "no pod is Ready yet" fast requeue is intentionally NOT returned
	// here. reconcilePools runs in the Phase-2 resource loop; a RequeueAfter from
	// it aborts that loop, so the reconcile returns before Phase 3 ever runs the
	// cluster reconcilers (maintenance-mode clear, stale-disk wipe, decommission) —
	// which must run precisely while brokers are unready during an outage. The
	// readiness poll is applied at the end of Reconcile instead — see
	// PoolTracker.ScaledUpButNoneReady and the tail of Reconcile.
	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) initAdminClient(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)
	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, state.cluster.StretchCluster)
	if err != nil {
		if internalclient.IsNoRepresentativePoolError(err) {
			// it may happen when there's no BrokerPool for this StretchCluster yet available
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
		logger.Error(err, "error fetching redpanda admin client")
		return ctrl.Result{}, err
	}
	state.admin = admin
	// Deferred + memoized so a pass renders the per-pod endpoint list at most
	// once, and only when a remediation step actually needs an endpoint.
	state.podEndpoints = memoizeEndpoints(func() []string {
		return r.LifecycleClient.GetAdminAPIEndpoints(state.cluster)
	})
	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcileDecommission(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
	var health rpadmin.ClusterHealthOverview

	ctx, span := trace.Start(ctx, "reconcileDecommission")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error decommissioning brokers")
			if internalclient.IsTerminalClientError(err) {
				state.status.StretchClusterStatus.SetHealthy(statuses.StretchClusterHealthyReasonTerminalError, err.Error())
			} else {
				state.status.StretchClusterStatus.SetHealthy(statuses.StretchClusterHealthyReasonError, err.Error())
			}
			trace.EndSpan(span, err)
			return
		}

		if state.pools.AllZero() {
			state.status.StretchClusterStatus.SetHealthy(statuses.StretchClusterHealthyReasonNotHealthy, messageNoBrokers)
		} else if health.IsHealthy {
			state.status.StretchClusterStatus.SetHealthy(statuses.StretchClusterHealthyReasonHealthy)
		} else {
			state.status.StretchClusterStatus.SetHealthy(statuses.StretchClusterHealthyReasonNotHealthy, "Cluster is not healthy")
		}
		trace.EndSpan(span, err)
	}()

	health, err = r.fetchClusterHealth(ctx, state.admin)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "fetching cluster health")
	}

	// Record the cluster-wide replication health gauge alongside the existing
	// condition write. Done here (rather than in the defer above) because we
	// only have an authoritative answer once the admin API call has returned;
	// the defer fires on every code path including errors where `health` is
	// the zero value.
	observability.RecordStretchClusterReplicationHealth(state.cluster.Name, health.IsHealthy)

	// health.NodesDown is treated as an advisory hint, not authority.
	// The cluster health overview is the controller broker's consensus view of
	// who has missed heartbeats. Right after a partition (or controller
	// re-election) it can transiently list brokers that are actually alive —
	// observed in the lab as cluster three's brokers being flagged "down"
	// alongside the genuinely-partitioned cluster two brokers. The per-broker
	// gossip endpoint (/v1/brokers) doesn't help here because it's
	// broker-local: each broker reports its own opinion, not consensus.
	// Skipping a wrongly-reported "down" broker would leave it out of the
	// brokerMap used by scaleDown for decommission pod-lookup, breaking
	// decommission safety.
	//
	// Instead, attempt the Broker fetch for every node with a tight per-call
	// timeout. Truly unreachable brokers fail fast (or hit the timeout);
	// brokers wrongly listed in NodesDown succeed and end up in the map. The
	// timeout keeps reconcileClusterConfig and later phases from being
	// gated on a 10s (or whatever the admin-client default is) hang per
	// down broker.
	//
	// brokerMap keys brokers by the first DNS label of their internal RPC
	// address (pod name for single-cluster) and also by the raw host (pod IP
	// for stretch-cluster flat-network mode where InternalRPCAddress is an IP).
	// Values are buckets, not a single broker ID: a StretchCluster with
	// identically-named BrokerPools across member clusters can legitimately
	// produce the same pod name in more than one member cluster (a
	// StatefulSet/pod name has no member-cluster component), so
	// brokerIDForPod treats a bucket with more than one entry as ambiguous
	// rather than guessing (see its doc comment) instead of the operator
	// decommissioning, or skipping the pre-restart probe for, the wrong broker.
	//nolint:laconiccomments
	brokerMap := map[string][]int{}
	downNodes := map[int]bool{}
	for _, brokerID := range health.AllNodes {
		brokerCtx, brokerCancel := context.WithTimeout(ctx, brokerFetchTimeout)
		broker, err := state.admin.Broker(brokerCtx, brokerID)
		brokerCancel()
		if err != nil {
			logger.Info("broker unreachable, omitting from brokerMap (will be treated as down for decommission)", "brokerID", brokerID, "error", err)
			downNodes[brokerID] = true
			continue
		}

		host := broker.InternalRPCAddress
		// InternalRPCAddress may be "host:port" or just "host".
		if h, _, err := net.SplitHostPort(broker.InternalRPCAddress); err == nil {
			host = h
		}
		if host == "" {
			continue
		}
		// Key by first DNS label (pod name for single-cluster FQDN).
		brokerMap[strings.SplitN(host, ".", 2)[0]] = append(brokerMap[strings.SplitN(host, ".", 2)[0]], brokerID)
		// Also key by the full host (pod IP for stretch-cluster flat-network).
		brokerMap[host] = append(brokerMap[host], brokerID)
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range state.pools.ToScaleDown() {
		// Until the previous step's pod is gone it is still LastPod, and its
		// decommissioned broker reads as already removed, so scaleDown would
		// drop the next broker without decommissioning it.
		if !state.pools.CheckScale(ctx) {
			logger.V(log.DebugLevel).Info("previous scale-down step still settling, deferring")
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
		requeue, err := r.scaleDown(ctx, state.admin, state.cluster, set, brokerMap, downNodes)
		result := ctrl.Result{}
		if requeue {
			result.RequeueAfter = requeueTimeout
		}
		//nolint:staticcheck // SA4004 this is intentionally early terminated
		return result, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range state.pools.ToDelete() {
		logger.V(log.TraceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String(), "cluster", set.GetCanonicalClusterName())
		if err := r.LifecycleClient.DeleteStatefulSetForBrokerPool(ctx, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting statefulset")
		}
	}

	if state.pools.AllZero() {
		// When there is no desired node pool the cluster is tearing down, so
		// no pods needs to be roll over.
		logger.V(log.TraceLevel).Info("no desired broker pool found")
		return reconcile.Result{}, nil
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := state.pools.PodsToRoll()
	logger.V(log.DebugLevel).Info("rolling pods", "rollSet", len(rollSet), "brokerMap", brokerMap, "isHealthy", health.IsHealthy)

	// Don't start rolling while a recently replaced pod is still coming up.
	// The cluster health view (brokerMap, isHealthy) lags behind pod state,
	// and rolling a second pod before the first one's replacement is ready
	// would cause two pods to be unavailable simultaneously.
	// Only check when there are actually pods to roll — otherwise we'd block
	// normal reconciliation when a pod is unready for unrelated reasons.
	if len(rollSet) > 0 && state.pools.HasRecentlyReplacedPods() {
		logger.V(log.DebugLevel).Info("recently replaced pods not ready, deferring rolling restart")
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	// Post-restart recovery gate (Redpanda 25.1+), mirroring RedpandaReconciler:
	// even when the prior pod is K8s-Ready the broker inside it may still be
	// replaying partition state from its peers. Wait for every broker to report
	// load_reclaimed_pc >= threshold before rolling the next one. These are live
	// admin-API calls (not informer reads), so they don't add the cache-staleness
	// concern stretch's multiple informers otherwise carry. On <25.1 the endpoint
	// 404s and brokersStillRecovering reports not-recovering, so behavior is
	// unchanged there.
	if len(rollSet) > 0 {
		recovering, err := brokersStillRecovering(ctx, state.admin, brokerMap, r.PostRestartCaughtUpPercent, logger)
		switch {
		case err != nil:
			// Fail closed — a non-404 probe error means we can't confirm the
			// just-restarted broker finished recovering, so defer rather than
			// risk rolling the next broker mid-recovery. (Bounded retry/backoff
			// for transient errors is handled by the rpadmin client.)
			logger.V(log.DebugLevel).Info("post-restart probe error, deferring rolling restart", "error", err)
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		case recovering:
			logger.V(log.DebugLevel).Info("a broker is still post-restart recovering, deferring rolling restart")
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
	}

	// Schema Registry sync gate, mirroring RedpandaReconciler: a rolled
	// broker serves Kafka as soon as it restarts, but its SR store replays
	// _schemas before answering consistently (INC-2903: ~13 minutes on large
	// topics). Probing every broker's SR listener via /status/ready —
	// auth-exempt, blocking until the store is caught up — serializes the
	// roll against SR sync the same way the post-restart probe serializes it
	// against partition recovery. The factory discovers per-broker SR
	// endpoints across all member clusters, so the probe covers remote pools
	// too. Fail closed like the gate above; escape hatch:
	// --wait-for-schema-registry-sync=false. Skipped when the pools' SR
	// listener configs would make per-broker probing unsound — see
	// internalclient.SchemaRegistryProbeUniform. The uniformity check runs
	// on the reconciler's cached pool state rather than re-listing pools.
	if len(rollSet) > 0 && r.WaitForSchemaRegistrySync {
		if uniform, reason := internalclient.SchemaRegistryProbeUniform(state.cluster.GetAllBrokerPools()); !uniform {
			logger.V(log.DebugLevel).Info("skipping schema registry sync roll gate", "reason", reason)
		} else {
			satisfied := schemaRegistryReady(ctx, r.ClientFactory, state.cluster.StretchCluster, logger)
			if !satisfied {
				return ctrl.Result{RequeueAfter: requeueTimeout}, nil
			}
		}
	}

	rolled := false
	for _, pod := range rollSet {
		// Same roll-safety decision as the single-cluster RedpandaReconciler:
		// resolve the pod to a broker (pod name, then pod IP for stretch
		// flat-network mode where InternalRPCAddress is a bare IP),
		// pre-restart-probe it when mapped, then apply decideRollAction. These
		// are live admin-API checks, not informer reads, so they don't add the
		// cache-staleness concern stretch's multiple informers otherwise carry.
		brokerID, inBrokerMap, ambiguous := brokerIDForPod(brokerMap, pod.GetName(), pod.Status.PodIP)
		if ambiguous {
			// Do NOT treat this as "not in brokerMap" — decideRollAction
			// deletes an unmapped pod outright when the cluster is healthy,
			// which would bypass the pre-restart safety probe for a pod that
			// in fact maps to more than one live broker across member
			// clusters.
			logger.Info("skipping roll decision: pod name matches multiple brokers, refusing to guess which one it is", "pod", pod.GetName(), "cluster", pod.GetCluster())
			continue
		}
		var brokerSafe bool
		var probeErr error
		if inBrokerMap {
			brokerSafe, probeErr = brokerSafeToRestart(ctx, state.admin, brokerID, health.IsHealthy, logger, pod.GetName())
		}

		shouldRoll, continueExecution, reason := decideRollAction(inBrokerMap, health.IsHealthy, brokerSafe, probeErr)
		logArgs := []any{"pod", pod.GetName(), "cluster", pod.GetCluster(), "inBrokerMap", inBrokerMap, "isHealthy", health.IsHealthy, "shouldRoll", shouldRoll, "reason", reason}
		if probeErr != nil {
			logArgs = append(logArgs, "error", probeErr)
		}
		logger.V(log.DebugLevel).Info("pod roll decision", logArgs...)

		if shouldRoll {
			rolled = true
			logger.V(log.TraceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String(), "cluster", pod.GetCluster())

			if err := r.LifecycleClient.DeletePod(ctx, pod); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "deleting pod")
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return ctrl.Result{RequeueAfter: requeueTimeout}, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcileLicense(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ ctrl.Result, err error) {
	var license *redpandav1alpha2.RedpandaLicenseStatus

	ctx, span := trace.Start(ctx, "reconcileLicense")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			if internalclient.IsTerminalClientError(err) {
				state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonTerminalError, err.Error())
			} else {
				state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonError, err.Error())
			}
		} else if license != nil {
			state.status.LicenseStatus = license
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonNotPresent, messageNoBrokers)
		return ctrl.Result{}, nil
	}

	// rate limit license reconciliation
	if statuses.HasRecentCondition(state.cluster.StretchCluster, statuses.StretchClusterLicenseValid, metav1.ConditionTrue, time.Minute) {
		// just copy over the license status from our existing status
		state.status.StretchClusterStatus.SetLicenseValidFromCurrent(state.cluster.StretchCluster)
		return ctrl.Result{}, nil
	}

	if err := r.setupLicense(ctx, state.cluster.StretchCluster, state.admin); err != nil {
		logger.Error(err, "error setting up license")
		return ctrl.Result{}, errors.WithStack(err)
	}

	features, err := state.admin.GetEnterpriseFeatures(ctx)
	if err != nil {
		logger.Error(err, "error getting enterprise features")
		return ctrl.Result{}, errors.WithStack(err)
	}

	licenseInfo, err := state.admin.GetLicenseInfo(ctx)
	if err != nil {
		logger.Error(err, "error getting license info")
		return ctrl.Result{}, errors.WithStack(err)
	}

	switch features.LicenseStatus {
	case rpadmin.LicenseStatusExpired:
		state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonExpired)
	case rpadmin.LicenseStatusNotPresent:
		state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonNotPresent)
	case rpadmin.LicenseStatusValid:
		state.status.StretchClusterStatus.SetLicenseValid(statuses.StretchClusterLicenseValidReasonValid)
	}

	licenseStatus := func() *redpandav1alpha2.RedpandaLicenseStatus {
		inUseFeatures := []string{}
		for _, feature := range features.Features {
			if feature.Enabled {
				inUseFeatures = append(inUseFeatures, feature.Name)
			}
		}
		// features.Features iterates a map under the hood; sort the
		// resulting slice so repeated reconciles produce byte-identical
		// status. Without this, the slice order flips on every write,
		// bumps resourceVersion, and triggers a watch-driven hot
		// reconcile loop on the StretchCluster controller.
		sort.Strings(inUseFeatures)

		status := &redpandav1alpha2.RedpandaLicenseStatus{
			InUseFeatures: inUseFeatures,
			Violation:     features.Violation,
		}

		// make sure we can actually format the extend license properties
		if !licenseInfo.Loaded {
			return status
		}

		status.Organization = ptr.To(licenseInfo.Properties.Organization)
		status.Type = ptr.To(licenseInfo.Properties.Type)
		expirationTime := time.Unix(licenseInfo.Properties.Expires, 0)

		// if we have an expiration that is below 0 we are already expired
		// so no need to set the expiration time
		status.Expired = ptr.To(licenseInfo.Properties.Expires <= 0 || expirationTime.Before(time.Now()))

		if !*status.Expired {
			status.Expiration = &metav1.Time{Time: expirationTime.UTC()}
		}

		return status
	}

	license = licenseStatus()
	return ctrl.Result{}, nil
}

func (r *MulticlusterReconciler) reconcileClusterConfig(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (_ reconcile.Result, err error) {
	ctx, span := trace.Start(ctx, "reconcileClusterConfig")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			if internalclient.IsTerminalClientError(err) {
				state.status.StretchClusterStatus.SetConfigurationApplied(statuses.StretchClusterConfigurationAppliedReasonTerminalError, err.Error())
			} else {
				state.status.StretchClusterStatus.SetConfigurationApplied(statuses.StretchClusterConfigurationAppliedReasonError, err.Error())
			}
		}
		trace.EndSpan(span, err)
	}()

	if state.pools.AllZero() {
		state.status.StretchClusterStatus.SetConfigurationApplied(statuses.StretchClusterConfigurationAppliedReasonNotApplied, messageNoBrokers)
		return ctrl.Result{}, nil
	}

	// we rate limit setting cluster configuration to once a minute if it's already been applied and is up-to-date
	if statuses.HasRecentCondition(state.cluster.StretchCluster, statuses.StretchClusterConfigurationApplied, metav1.ConditionTrue, time.Minute) {
		state.status.StretchClusterStatus.SetConfigurationApplied(statuses.StretchClusterConfigurationAppliedReasonApplied)
		return ctrl.Result{}, nil
	}

	config := state.cluster.StretchCluster.Spec.GetClusterConfig()
	if config == nil {
		config = map[string]any{}
	}

	superusers, err := r.superusersFor(ctx, state, cluster)
	if err != nil {
		logger.Error(err, "fetching super users")
		return ctrl.Result{}, errors.WithStack(err)
	}

	mode := feature.ClusterConfigSyncMode.Get(ctx, state.cluster.StretchCluster)

	syncer := syncclusterconfig.Syncer{Client: state.admin, Mode: mode}
	configStatus, err := syncer.Sync(ctx, config, superusers)
	if err != nil {
		logger.Error(err, "syncing cluster config")
		return ctrl.Result{}, errors.WithStack(err)
	}

	state.status.StretchClusterStatus.SetConfigurationApplied(statuses.StretchClusterConfigurationAppliedReasonApplied)

	version := configStatus.PropertiesThatNeedRestartHash
	didConfigChange := state.cluster.StretchCluster.Status.ConfigVersion != version
	state.status.ConfigVersion = ptr.To(version)

	logger.Info("cluster config sync result",
		"configVersion", version,
		"previousConfigVersion", state.cluster.StretchCluster.Status.ConfigVersion,
		"didConfigChange", didConfigChange,
		"restartOnConfigChange", state.restartOnConfigChange,
		"needsRestart", configStatus.NeedsRestart,
	)

	result := ctrl.Result{}
	if didConfigChange && state.restartOnConfigChange {
		logger.Info("config version changed, requeuing for rolling restart")
		result.RequeueAfter = requeueTimeout
	}

	return result, nil
}

func (r *MulticlusterReconciler) superusersFor(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) ([]string, error) {
	sc := state.cluster.StretchCluster

	if sc.Spec.Auth == nil || sc.Spec.Auth.SASL == nil || !ptr.Deref(sc.Spec.Auth.SASL.Enabled, false) {
		return nil, nil
	}

	superusers := []string{}

	if sc.Spec.Auth.SASL.SecretRef != nil {
		key := client.ObjectKey{Namespace: sc.Namespace, Name: *sc.Spec.Auth.SASL.SecretRef}

		var users corev1.Secret
		if err := cluster.GetClient().Get(ctx, key, &users); err != nil {
			return nil, errors.WithStack(err)
		}

		for filename, userTXT := range users.Data {
			superusers = append(superusers, syncclusterconfig.LoadUsersFile(ctx, filename, userTXT)...)
		}
	}

	// The bootstrap user was already set by syncBootstrapUser earlier in the reconciliation chain.
	if state.bootstrapUser != "" {
		superusers = append(superusers, state.bootstrapUser)
	}

	return syncclusterconfig.NormalizeSuperusers(superusers), nil
}

func (r *MulticlusterReconciler) syncStatus(ctx context.Context, _ cluster.Cluster, state *stretchClusterReconciliationState, result ctrl.Result, err error) (ctrl.Result, error) {
	original := state.cluster.StretchCluster.Status.DeepCopy()
	if r.LifecycleClient.SetClusterStatus(state.cluster, state.status) {
		log.FromContext(ctx).V(log.TraceLevel).Info("setting cluster status from diff", "original", original, "new", state.cluster.StretchCluster.Status)

		// Update status on all clusters, not just the triggering one,
		// since the StretchCluster CRD exists on every cluster.
		// Unreachable clusters are skipped — status will be propagated
		// once they reconnect.
		logger := log.FromContext(ctx)
		for _, clusterName := range r.Manager.GetClusterNames() {
			if !r.Manager.IsClusterReachable(clusterName) {
				logger.V(log.DebugLevel).Info("cluster unreachable, skipping status update", "cluster", clusterName)
				continue
			}

			cl, clErr := r.Manager.GetCluster(ctx, clusterName)
			if clErr != nil {
				logger.Info("cluster unreachable, skipping status update", "cluster", clusterName, "error", clErr)
				continue
			}

			// Fetch the latest version from this cluster to get the correct resource version.
			// Skip clusters where the SC doesn't exist (e.g. partial deletion).
			remoteSC := &redpandav1alpha2.StretchCluster{}
			if clErr := cl.GetClient().Get(ctx, client.ObjectKeyFromObject(state.cluster.StretchCluster), remoteSC); clErr != nil {
				if client.IgnoreNotFound(clErr) != nil {
					logger.Info("could not fetch StretchCluster for status update, skipping", "cluster", clusterName, "error", clErr)
				}
				continue
			}

			remoteSC.Status = *state.cluster.StretchCluster.Status.DeepCopy()
			if syncErr := cl.GetClient().Status().Update(ctx, remoteSC); syncErr != nil {
				err = errors.Join(err, errors.Wrapf(syncErr, "updating status on cluster %s", clusterName))
			}
		}
	}

	syncResult, syncErr := ignoreConflict(err)
	if syncErr == nil && (result.RequeueAfter > 0) {
		syncResult.RequeueAfter = result.RequeueAfter
	}
	return syncResult, syncErr
}

func (r *MulticlusterReconciler) fetchClusterHealth(ctx context.Context, admin *rpadmin.AdminAPI) (_ rpadmin.ClusterHealthOverview, err error) {
	ctx, span := trace.Start(ctx, "fetchClusterHealth")
	defer func() { trace.EndSpan(span, err) }()

	var health rpadmin.ClusterHealthOverview

	if admin == nil {
		log.FromContext(ctx).Error(errors.New("no admin api"), "Admin must be initialized")
		return health, nil
	}

	health, err = admin.GetHealthOverview(ctx)
	if err != nil {
		return health, err
	}

	return health, nil
}

// scaleDown contains the majority of the logic for scaling down a statefulset incrementally, first
// decommissioning the broker with the last pod ordinal and then patching the statefulset with
// a single less replica.
func (r *MulticlusterReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.StretchClusterWithPools, set *lifecycle.ScaleDownSet, brokerMap map[string][]int, downNodes map[int]bool) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("MulticlusterReconciler[%T].scaleDown", *cluster))
	logger.V(log.TraceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// Resolve by pod name, then pod IP (stretch flat-network advertises brokers
	// by bare IP) so a live broker is decommissioned before its pod is removed
	// rather than being misclassified as already-removed.
	brokerID, ok, ambiguous := brokerIDForPod(brokerMap, set.LastPod.GetName(), set.LastPod.Status.PodIP)
	if ambiguous {
		// Do NOT fall through to the !ok branch below: that path assumes the
		// broker was already fully removed from the cluster and proceeds to
		// patch the StatefulSet directly. Here the pod maps to more than one
		// real broker (identically-named BrokerPools across member
		// clusters), so guessing could decommission — or silently orphan —
		// the wrong one.
		logger.Info("deferring scale-down: pod name matches multiple brokers, refusing to guess which one to decommission",
			"pod", client.ObjectKeyFromObject(set.LastPod).String())
		return true, nil
	}
	if ok {
		// decommission if we have a brokerID, if not
		// then the node has already been fully removed from
		// the cluster and we can go ahead and delete the pod
		// through patching the statefulset

		requeue, err := r.decommissionBroker(ctx, admin, cluster, set, brokerID)
		if err != nil {
			return false, err
		}

		if requeue {
			return true, nil
		}
	} else {
		// The pod isn't in brokerMap. Two cases:
		//   1. The broker was fully removed from the cluster — safe to delete
		//      the pod (the brokerMap absence reflects ground truth).
		//   2. The broker sits on a partitioned peer and was skipped earlier
		//      to keep reconcileDecommission from hanging. Deleting the pod
		//      now would orphan the broker on heal. Requeue and wait.
		// Distinguish by checking whether ANY known broker for this pool sits
		// in downNodes — if so, treat the absence as transient and requeue.
		if len(downNodes) > 0 {
			logger.Info("pod missing from brokerMap while peer brokers are down, requeueing scale-down to avoid orphaning a partitioned broker",
				"pod", set.LastPod.GetName(), "downNodes", downNodes)
			return true, nil
		}
	}

	logger.V(log.TraceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// now patch the statefulset to remove the pod
	if err := r.LifecycleClient.PatchPoolSet(ctx, cluster, set.StatefulSet); err != nil {
		return false, errors.Wrap(err, "scaling down statefulset")
	}
	// we only do a statefulset at a time, waiting for them to
	// become stable first
	return true, nil
}

// decommissionBroker handles decommissioning a broker and waiting until it has finished decommissioning
func (r *MulticlusterReconciler) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.StretchClusterWithPools, set *lifecycle.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("MulticlusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(log.TraceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(log.TraceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, errors.Wrap(err, "decommissioning broker")
			}

			return true, nil
		}
		return false, errors.Wrap(err, "fetching decommission status")
	}
	if !decommissionStatus.Finished {
		logger.V(log.TraceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

		// just requeue since we're still decommissioning
		return true, nil
	}

	// we're finished
	return false, nil
}

func (r *MulticlusterReconciler) setupLicense(ctx context.Context, sc *redpandav1alpha2.StretchCluster, adminClient *rpadmin.AdminAPI) error {
	if sc.Spec.Enterprise == nil {
		return nil
	}

	if literalLicense := ptr.Deref(sc.Spec.Enterprise.License, ""); literalLicense != "" {
		if err := adminClient.SetLicense(ctx, strings.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	if secretReference := sc.Spec.Enterprise.LicenseSecretRef; secretReference != nil {
		name := ptr.Deref(secretReference.Name, "")
		key := ptr.Deref(secretReference.Key, "")
		if name == "" || key == "" {
			return errors.Newf("both name %q and key %q of the secret can not be empty string", name, key)
		}

		// Search all clusters for the license secret since the user may have
		// created it on any cluster.
		var licenseSecret *corev1.Secret
		for _, clusterName := range r.Manager.GetClusterNames() {
			cl, err := r.Manager.GetCluster(ctx, clusterName)
			if err != nil {
				continue
			}
			var secret corev1.Secret
			if err := cl.GetClient().Get(ctx, client.ObjectKey{Namespace: sc.Namespace, Name: name}, &secret); err == nil {
				licenseSecret = &secret
				break
			}
		}

		if licenseSecret == nil {
			return errors.Newf("license secret %q not found in any cluster", name)
		}

		literalLicense := licenseSecret.Data[key]
		if err := adminClient.SetLicense(ctx, bytes.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager, redpandaImage lifecycle.Image, sidecarImage lifecycle.Image, cloudSecrets lifecycle.CloudSecretsFlags, factory *internalclient.Factory, reconcileTimeout time.Duration, brokerPodNodeUnavailableToleration time.Duration, postRestartCaughtUpPercent int, waitForSchemaRegistrySync bool, clearMaintenanceModeAfter time.Duration, staleDiskWipeNotReadyThreshold time.Duration) error {
	// A nil factory survives until the first reconcile that builds an admin
	// client, then panics on every such pass (Factory methods dispatch fine on
	// a nil receiver until the first field access). Refuse it at startup.
	if factory == nil {
		return errors.New("SetupMulticlusterController requires a non-nil client factory")
	}
	lifecycleClient := lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(redpandaImage, sidecarImage, cloudSecrets)).WithBrokerPodNodeUnavailableToleration(brokerPodNodeUnavailableToleration)

	builder := mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
>>>>>>> fde0a00c (operator: settle each scale-down step before decommissioning the next (#1998))
		// NB: This is gross, but currently the multicluster runtime doesn't hand this global option off to the controller
		// registration properly, so we can't boot multiple controllers in test without doing this.
		// Consider an upstream fix.
		SkipNameValidation: ptr.To(true),
	}).For(&redpandav1alpha2.StretchCluster{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true)).Complete(&MulticlusterReconciler{manager: mgr})
}
