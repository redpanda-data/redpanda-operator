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
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/otelkube"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	syncclusterconfig "github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	rendermulticluster "github.com/redpanda-data/redpanda-operator/operator/multicluster"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

const (
	bootstrapUserSecretSuffix = "-bootstrap-user"
	bootstrapUserPasswordKey  = "password"
	defaultBootstrapUsername  = "kubernetes-controller"
	caOrganization            = "Redpanda"
)

//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cluster.redpanda.com,resources=stretchclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=serviceexports,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=serviceimports,verbs=get;list;watch;create;update;patch;delete

type MulticlusterReconciler struct {
	Manager         multicluster.Manager
	LifecycleClient *lifecycle.ResourceClient[lifecycle.StretchClusterWithPools, *lifecycle.StretchClusterWithPools]
	ClientFactory   internalclient.ClientFactory
	UseNodePools    bool
}

type stretchClusterReconciliationState struct {
	cluster               *lifecycle.StretchClusterWithPools
	pools                 *lifecycle.PoolTracker
	status                *lifecycle.ClusterStatus
	restartOnConfigChange bool
	admin                 *rpadmin.AdminAPI
	bootstrapUser         string
	bootstrapPassword     string
}

func (s *stretchClusterReconciliationState) cleanup() {
	if s.admin != nil {
		s.admin.Close()
	}
}

type stretchClusterReconciliationFn func(ctx context.Context, state *stretchClusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error)

func (r *MulticlusterReconciler) Reconcile(ctx context.Context, req mcreconcile.Request) (result ctrl.Result, err error) {
	start := time.Now()
	l := log.FromContext(ctx).WithName("MulticlusterReconciler.Reconcile")

	l.V(1).Info("Starting reconcile loop")
	defer func() {
		l.V(1).Info("Finished reconciling", "elapsed", time.Since(start))
	}()

	cluster, err := r.Manager.GetCluster(ctx, req.ClusterName)
	if err != nil {
		l.Error(err, "unable to fetch cluster, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	k8sClient := cluster.GetClient()

	stretchCluster := &redpandav1alpha2.StretchCluster{}
	if err := k8sClient.Get(ctx, req.NamespacedName, stretchCluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer func() {
		// If we have a resource to manage, ensure that we re-enqueue to re-examine it on a regular basis
		if err != nil {
			// Error returns cause a re-enqueuing this with exponential backoff
			return
		}

		if result.RequeueAfter > 0 {
			// We're already set up to enqueue this resource again
			return
		}

		result.RequeueAfter = periodicRequeue
	}()

	ctx, span := trace.Start(otelkube.Extract(ctx, stretchCluster), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
		attribute.String("clusterName", req.ClusterName),
	))
	defer func() { trace.EndSpan(span, err) }()

	// TODO Should it be removed?
	if !feature.V2Managed.Get(ctx, stretchCluster) {
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				l.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	state, err := r.fetchInitialState(ctx, stretchCluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer state.cleanup()

	// Examine if the object is under deletion
	if !stretchCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		l.V(log.TraceLevel).Info("deletion timestamp is not zero. Resources cleanup and remove finalizer")
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, state.cluster); deleted || err != nil {
			return r.syncStatus(ctx, cluster, state, ctrl.Result{}, err)
		}
		if controllerutil.RemoveFinalizer(stretchCluster, FinalizerKey) {
			if err := k8sClient.Update(ctx, stretchCluster); err != nil {
				l.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Check that .spec is consistent across all clusters before proceeding.
	// If there's drift, block reconciliation until the user fixes it.
	if drifted, driftResult, driftErr := r.checkSpecConsistency(ctx, state, stretchCluster, req.ClusterName); drifted || driftErr != nil {
		return r.syncStatus(ctx, cluster, state, driftResult, driftErr)
	}

	// Update our StretchCluster with our finalizer and any default Annotation FFs.
	// If any changes are made, persist the changes and immediately requeue to
	// prevent any cache / resource version synchronization issues.
	if controllerutil.AddFinalizer(stretchCluster, FinalizerKey) || feature.SetDefaults(ctx, feature.V2Flags, stretchCluster) {
		if err := k8sClient.Update(ctx, stretchCluster); err != nil {
			l.Error(err, "updating cluster finalizer or Annotation")
			return ignoreConflict(err)
		}
		return ctrl.Result{RequeueAfter: finalizerRequeueTimeout}, nil
	}

	// Phase 1: cross-cluster syncers — ensure shared resources exist in all k8s clusters
	// before any per-cluster reconciliation begins.
	syncers := []stretchClusterReconciliationFn{
		r.syncBootstrapUser,
		r.syncCA,
	}

	for _, syncer := range syncers {
		result, err := syncer(ctx, state, cluster)
		if err != nil || result.RequeueAfter > 0 {
			l.V(log.TraceLevel).Info("aborting reconciliation early during sync phase", "error", err, "requeueAfter", result.RequeueAfter)
			return r.syncStatus(ctx, cluster, state, result, err)
		}
	}

	// Phase 2: sync Kubernetes resources (simple resources + node pools).
	// EndpointSlice sync runs between resource creation and pool readiness
	// checks so that pods can discover each other before becoming ready.
	resourceReconcilers := []stretchClusterReconciliationFn{
		r.reconcileResources,
		r.reconcilePools,
	}
	for _, reconciler := range resourceReconcilers {
		result, err := reconciler(ctx, state, cluster)
		if err != nil || result.RequeueAfter > 0 {
			l.V(log.TraceLevel).Info("aborting reconciliation early during resource sync", "error", err, "requeueAfter", result.RequeueAfter)
			return r.syncStatus(ctx, cluster, state, result, err)
		}
	}

	// All Kubernetes resources are in sync.
	state.status.StretchClusterStatus.SetResourcesSynced(statuses.StretchClusterResourcesSyncedReasonSynced)

	// Phase 3: cluster-level reconciliation (admin API, decommission, config, license).
	clusterReconcilers := []stretchClusterReconciliationFn{
		r.initAdminClient,
		r.reconcileDecommission,
		r.reconcileLicense,
		r.reconcileClusterConfig,
	}
	for _, reconciler := range clusterReconcilers {
		result, err := reconciler(ctx, state, cluster)
		if err != nil || result.RequeueAfter > 0 {
			l.V(log.TraceLevel).Info("aborting reconciliation early", "error", err, "requeueAfter", result.RequeueAfter)
			return r.syncStatus(ctx, cluster, state, result, err)
		}
	}

	// we're at the end of reconciliation, so sync back our status
	l.V(log.TraceLevel).Info("finished normal reconciliation loop")
	return r.syncStatus(ctx, cluster, state, ctrl.Result{}, nil)
}

// checkSpecConsistency fetches the StretchCluster from every known cluster and
// verifies that .Spec is identical. If drift is detected it sets SpecSynced=False,
// persists the status, and returns drifted=true so the caller can abort reconciliation.
// When specs are aligned it sets SpecSynced=True.
func (r *MulticlusterReconciler) checkSpecConsistency(ctx context.Context, state *stretchClusterReconciliationState, sc *redpandav1alpha2.StretchCluster, localClusterName string) (drifted bool, _ ctrl.Result, _ error) {
	l := log.FromContext(ctx).WithName("checkSpecConsistency")

	localSpec := sc.Spec
	var driftDetails []string

	for _, clusterName := range r.Manager.GetClusterNames() {
		// Skip the local cluster — we already have its spec.
		if clusterName == localClusterName {
			continue
		}
		remote, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			return false, ctrl.Result{}, errors.Wrapf(err, "fetching cluster %q for spec consistency check", clusterName)
		}

		remoteSC := &redpandav1alpha2.StretchCluster{}
		if err := remote.GetClient().Get(ctx, client.ObjectKeyFromObject(sc), remoteSC); err != nil {
			return false, ctrl.Result{}, errors.Wrapf(err, "fetching StretchCluster from cluster %q", clusterName)
		}

		if !apiequality.Semantic.DeepEqual(localSpec, remoteSC.Spec) {
			fields := specDiffFields(localSpec, remoteSC.Spec)
			driftDetails = append(driftDetails, fmt.Sprintf("%s (fields: %s)", clusterName, strings.Join(fields, ", ")))
		}
	}

	if len(driftDetails) == 0 {
		state.status.StretchClusterStatus.SetSpecSynced(statuses.StretchClusterSpecSyncedReasonSynced)
		return false, ctrl.Result{}, nil
	}

	msg := fmt.Sprintf("StretchCluster .spec differs on clusters: %s — reconciliation is blocked until specs are aligned", strings.Join(driftDetails, "; "))
	l.Info(msg)

	state.status.StretchClusterStatus.SetSpecSynced(statuses.StretchClusterSpecSyncedReasonDriftDetected, msg)
	return true, ctrl.Result{RequeueAfter: requeueTimeout}, nil
}

// specDiffFields returns the top-level JSON field names of StretchClusterSpec
// that differ between a and b.
func specDiffFields(a, b redpandav1alpha2.StretchClusterSpec) []string {
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	t := av.Type()

	var fields []string
	for i := range t.NumField() {
		if !apiequality.Semantic.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			name := t.Field(i).Name
			if tag, ok := t.Field(i).Tag.Lookup("json"); ok {
				if jsonName, _, _ := strings.Cut(tag, ","); jsonName != "" {
					name = jsonName
				}
			}
			fields = append(fields, name)
		}
	}
	return fields
}

func (r *MulticlusterReconciler) fetchInitialState(ctx context.Context, sc *redpandav1alpha2.StretchCluster) (*stretchClusterReconciliationState, error) {
	logger := log.FromContext(ctx)
	logger.V(log.DebugLevel).Info("fetchInitialState")

	sccluster := lifecycle.NewStretchClusterWithPools(sc, r.Manager.GetClusterNames())

	// grab NodePools from all connected clusters
	nodePools, err := r.LifecycleClient.FetchExistingNodePoolsFromAllClusters(ctx, sccluster)
	if err != nil {
		logger.Error(err, "fetching nodepools")
		return nil, err
	}
	sccluster.NodePools = nodePools

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	restartOnConfigChange := feature.RestartOnConfigChange.Get(ctx, sc)
	injectedConfigVersion := ""
	if restartOnConfigChange {
		injectedConfigVersion = sc.Status.ConfigVersion
	}
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, sccluster, injectedConfigVersion)
	if err != nil {
		logger.Error(err, "fetching pools")
		return nil, err
	}

	status := lifecycle.NewStretchClusterStatus()
	status.Pools = pools.PoolStatuses()

	if pools.AnyReady() {
		status.StretchClusterStatus.SetReady(statuses.StretchClusterReadyReasonReady)
	} else if pools.AllZero() {
		status.StretchClusterStatus.SetReady(statuses.StretchClusterReadyReasonNotReady, messageNoBrokers)
	} else {
		status.StretchClusterStatus.SetReady(statuses.StretchClusterReadyReasonNotReady, "No pods are ready")
	}

	// Populate pod endpoints on the cluster struct so the renderer can
	// produce Endpoints/EndpointSlices for flat network mode.
	sccluster.PodEndpoints = pools.PodEndpoints()

	return &stretchClusterReconciliationState{
		cluster:               sccluster,
		pools:                 pools,
		status:                status,
		restartOnConfigChange: restartOnConfigChange,
	}, nil
}

// bootstrapSecretName returns the name of the bootstrap user secret for a given
// stretch cluster, matching the convention used by the render path:
// <stretchcluster-name>-bootstrap-user. The same name is used in every k8s
// cluster so the StatefulSet env var can reference it without knowing the
// cluster name.
func bootstrapSecretName(sc *redpandav1alpha2.StretchCluster) string {
	return fmt.Sprintf("%s%s", sc.Name, bootstrapUserSecretSuffix)
}

func (r *MulticlusterReconciler) syncBootstrapUser(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (_ ctrl.Result, err error) {
	ctx, span := trace.Start(ctx, "syncBootstrapUser")
	logger := log.FromContext(ctx)

	defer func() {
		if err != nil {
			logger.Error(err, "error syncing bootstrap user")
		}
		trace.EndSpan(span, err)
	}()

	sc := state.cluster.StretchCluster

	if !sc.Spec.Auth.IsSASLEnabled() {
		logger.V(log.TraceLevel).Info("SASL is not enabled, skipping bootstrap user sync")
		return ctrl.Result{}, nil
	}
	clusterNames := r.Manager.GetClusterNames()

	// Phase 1: scan all clusters for existing bootstrap user secrets.
	// If multiple secrets exist, verify they all have the same password.
	var canonicalPassword string
	var canonicalCluster string
	for _, clusterName := range clusterNames {
		cl, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "getting cluster %s", clusterName)
		}

		secretName := bootstrapSecretName(sc)
		var existing corev1.Secret
		if err := cl.GetClient().Get(ctx, client.ObjectKey{
			Namespace: sc.Namespace,
			Name:      secretName,
		}, &existing); err == nil {
			if pw, ok := existing.Data[bootstrapUserPasswordKey]; ok && len(pw) > 0 {
				password := string(pw)
				if canonicalPassword == "" {
					canonicalPassword = password
					canonicalCluster = clusterName
					logger.V(log.TraceLevel).Info("found existing bootstrap user secret", "cluster", clusterName, "secret", secretName)
				} else if canonicalPassword != password {
					msg := fmt.Sprintf(
						"bootstrap user password mismatch: secret %q in cluster %q differs from cluster %q; "+
							"manual intervention required — delete the incorrect secret(s) and let the controller recreate them",
						secretName, clusterName, canonicalCluster,
					)
					state.status.StretchClusterStatus.SetBootstrapUserSynced(statuses.StretchClusterBootstrapUserSyncedReasonPasswordMismatch, msg)
					return ctrl.Result{}, errors.New(msg)
				}
			}
		}
	}

	// Phase 2: if no secret exists anywhere, generate a new password.
	generated := canonicalPassword == ""
	if generated {
		canonicalPassword = helmette.RandAlphaNum(32)
		logger.V(log.DebugLevel).Info("generated new bootstrap user password")
	}

	// Phase 3: ensure the secret exists in every cluster.
	for _, clusterName := range clusterNames {
		cl, err := r.Manager.GetCluster(ctx, clusterName)
		if err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "getting cluster %s", clusterName)
		}

		secretName := bootstrapSecretName(sc)
		k8sClient := cl.GetClient()

		var existing corev1.Secret
		if err := k8sClient.Get(ctx, client.ObjectKey{
			Namespace: sc.Namespace,
			Name:      secretName,
		}, &existing); err == nil {
			// Secret already exists — nothing to do for this cluster.
			continue
		}

		// Fetch the StretchCluster from this specific cluster to get the correct
		// UID for the owner reference (UIDs differ across clusters).
		localSC := &redpandav1alpha2.StretchCluster{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), localSC); err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "fetching StretchCluster from cluster %s for owner reference", clusterName)
		}

		labels := r.LifecycleClient.AddOwnerLabels(state.cluster)
		labels[lifecycle.GCLabel] = "false"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: sc.Namespace,
				Labels:    labels,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(localSC, redpandav1alpha2.SchemeGroupVersion.WithKind("StretchCluster")),
				},
			},
			Immutable: ptr.To(true),
			Type:      corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				bootstrapUserPasswordKey: []byte(canonicalPassword),
			},
		}

		if err := k8sClient.Create(ctx, secret); err != nil {
			return ctrl.Result{}, errors.Wrapf(err, "creating bootstrap user secret in cluster %s", clusterName)
		}
		logger.Info("created bootstrap user secret", "cluster", clusterName, "secret", secretName)
	}

	// Phase 4: set status condition.
	if generated {
		state.status.StretchClusterStatus.SetBootstrapUserSynced(statuses.StretchClusterBootstrapUserSyncedReasonSynced, fmt.Sprintf("Bootstrap user secret created and synced across %d cluster(s)", len(clusterNames)))
	} else {
		state.status.StretchClusterStatus.SetBootstrapUserSynced(statuses.StretchClusterBootstrapUserSyncedReasonExistingReused, fmt.Sprintf("Using existing bootstrap user secret from cluster %q", canonicalCluster))
	}

	state.bootstrapUser = defaultBootstrapUsername
	state.bootstrapPassword = canonicalPassword

	return ctrl.Result{}, nil
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
	managedCerts := rendermulticluster.BootstrappedCertNames(&defaultedSpec)
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
			cl, err := r.Manager.GetCluster(ctx, clusterName)
			if err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "getting cluster %s", clusterName)
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
			localSC := &redpandav1alpha2.StretchCluster{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(sc), localSC); err != nil {
				return ctrl.Result{}, errors.Wrapf(err, "fetching StretchCluster from cluster %s for CA owner reference", clusterName)
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
		logger.V(log.TraceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return ctrl.Result{RequeueAfter: requeueTimeout}, nil
	}

	logger.V(log.TraceLevel).Info("ready to scale and apply node pools", "existing", state.pools.ExistingStatefulSets(), "desired", state.pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range state.pools.ToCreate() {
		logger.V(log.TraceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "creating statefulset")
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range state.pools.ToScaleUp() {
		logger.V(log.TraceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "scaling up statefulset")
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range state.pools.RequiresUpdate() {
		logger.V(log.TraceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.LifecycleClient.PatchNodePoolSet(ctx, state.cluster, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "updating statefulset")
		}
	}

	result := ctrl.Result{}
	requeue := !(state.pools.AnyReady() || state.pools.AllZero())

	if requeue {
		logger.V(log.DebugLevel).Info("reconcilePools requeueAfter")
		result.RequeueAfter = requeueTimeout
	}
	return result, nil
}

func (r *MulticlusterReconciler) initAdminClient(ctx context.Context, state *stretchClusterReconciliationState, _ cluster.Cluster) (ctrl.Result, error) {
	if state.pools.AllZero() {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)
	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, state.cluster.StretchCluster)
	if err != nil {
		logger.Error(err, "error fetching redpanda admin client")
		return ctrl.Result{}, err
	}
	state.admin = admin
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

	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := state.admin.Broker(ctx, brokerID)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "fetching broker")
		}

		brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
		brokerMap[brokerTokens[0]] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range state.pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, state.admin, state.cluster, set, brokerMap)
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
		if err := r.LifecycleClient.DeleteStatefulSetForNodePool(ctx, set); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "deleting statefulset")
		}
	}

	if state.pools.AllZero() {
		// When there is no desired node pool the cluster is tearing down, so
		// no pods needs to be roll over.
		logger.V(log.TraceLevel).Info("no desired node pool found")
		return reconcile.Result{}, nil
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := state.pools.PodsToRoll()
	rolled := false
	for _, pod := range rollSet {
		shouldRoll, continueExecution := false, false

		if _, ok := brokerMap[pod.GetName()]; !ok {
			// we don't actually have this broker in the cluster
			// anymore, which means it's always safe to delete
			// the pod and continue with the next operations
			shouldRoll, continueExecution = true, true
		} else if health.IsHealthy {
			// TODO: don't just check overall cluster health, but use
			// scoped API endpoints for rolling a broker

			// roll and halt execution
			shouldRoll, continueExecution = true, false
		} else {
			// see if we can at least roll the next pods
			shouldRoll, continueExecution = false, true
		}

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
		for _, clusterName := range r.Manager.GetClusterNames() {
			cl, clErr := r.Manager.GetCluster(ctx, clusterName)
			if clErr != nil {
				err = errors.Join(err, errors.Wrapf(clErr, "getting cluster %s for status update", clusterName))
				continue
			}

			// Fetch the latest version from this cluster to get the correct resource version.
			remoteSC := &redpandav1alpha2.StretchCluster{}
			if clErr := cl.GetClient().Get(ctx, client.ObjectKeyFromObject(state.cluster.StretchCluster), remoteSC); clErr != nil {
				err = errors.Join(err, errors.Wrapf(clErr, "fetching StretchCluster from cluster %s for status update", clusterName))
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
func (r *MulticlusterReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.StretchClusterWithPools, set *lifecycle.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("MulticlusterReconciler[%T].scaleDown", *cluster))
	logger.V(log.TraceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	brokerID, ok := brokerMap[set.LastPod.GetName()]
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
	}

	logger.V(log.TraceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// now patch the statefulset to remove the pod
	if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set.StatefulSet); err != nil {
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

func SetupMulticlusterController(ctx context.Context, mgr multicluster.Manager, redpandaImage lifecycle.Image, sidecarImage lifecycle.Image, cloudSecrets lifecycle.CloudSecretsFlags, factory *internalclient.Factory) error {
	return mcbuilder.ControllerManagedBy(mgr).WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
		// NB: This is gross, but currently the multicluster runtime doesn't hand this global option off to the controller
		// registration properly, so we can't boot multiple controllers in test without doing this.
		// Consider an upstream fix.
		SkipNameValidation: ptr.To(true),
	}).For(
		&redpandav1alpha2.StretchCluster{},
		mcbuilder.WithEngageWithLocalCluster(true),
		mcbuilder.WithEngageWithProviderClusters(true)).
		Complete(
			&MulticlusterReconciler{
				Manager:         mgr,
				LifecycleClient: lifecycle.NewMulticlusterResourceClient(mgr, lifecycle.StretchClusterResourceManagers(redpandaImage, sidecarImage, cloudSecrets)),
				ClientFactory:   factory,
			},
		)
}
