// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package redpanda contains reconciliation logic for cluster.redpanda.com CRDs
package redpanda

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"go.opentelemetry.io/otel/attribute"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

const (
	FinalizerKey                    = "operator.redpanda.com/finalizer"
	RestartClusterOnConfigChangeKey = "operator.redpanda.com/restart-cluster-on-config-change"
	SyncerModeKey                   = "operator.redpanda.com/config-sync-mode"
	SyncerModeDeclarative           = "declarative"
	SyncerModeAdditive              = "additive" // The default for the moment

	NotManaged = "false"

	managedPath = "/managed"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"

	// reqeueueTimeout is the time that the reconciler will
	// wait before requeueing a cluster to be reconciled when
	// we've explicitly aborted a reconciliation loop early
	// due to an in-progress operation
	requeueTimeout = 10 * time.Second
)

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	// KubeConfig is the [rest.Config] that provides the go helm chart
	// Kubernetes access. It should be the same config used to create client.
	KubeConfig           *rest.Config
	Client               client.Client
	LifecycleClient      *lifecycle.ResourceClient[lifecycle.ClusterWithPools, *lifecycle.ClusterWithPools]
	EventRecorder        kuberecorder.EventRecorder
	ClientFactory        internalclient.ClientFactory
	CloudSecretsExpander *pkgsecrets.CloudExpander
}

// Any resource that the Redpanda helm chart creates and needs to reconcile.
// +kubebuilder:rbac:groups="",namespace=default,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=default,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,namespace=default,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=apps,namespace=default,resources=controllerrevisions,verbs=get;list;watch;
// +kubebuilder:rbac:groups=policy,namespace=default,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,namespace=default,resources=certificates,verbs=get;create;update;patch;delete;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,namespace=default,resources=issuers,verbs=get;create;update;patch;delete;list;watch
// +kubebuilder:rbac:groups="monitoring.coreos.com",namespace=default,resources=podmonitors;servicemonitors,verbs=get;list;watch;create;update;patch;delete

// Console chart
// +kubebuilder:rbac:groups=autoscaling,namespace=default,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,namespace=default,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// redpanda resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,namespace=default,resources=events,verbs=create;patch

// sidecar resources
// The leases is used by controller-runtime in sidecar. Operator main reconciliation needs to have leases permissions in order to create role that have the same permissions.
// +kubebuilder:rbac:groups=coordination.k8s.io,namespace=default,resources=leases,verbs=get;list;watch;create;update;patch;delete

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr)

	if err := r.LifecycleClient.WatchResources(builder, &redpandav1alpha2.Redpanda{}); err != nil {
		return err
	}

	return builder.Complete(r)
}

func (r *RedpandaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	rp := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	rp.ManagedFields = nil // nil out our managed fields

	cluster := lifecycle.NewClusterWithPools(rp)

	ctx, span := trace.Start(otelkube.Extract(ctx, rp), "Reconcile", trace.WithAttributes(
		attribute.String("name", req.Name),
		attribute.String("namespace", req.Namespace),
	))
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	if !isRedpandaManaged(ctx, rp) {
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := r.Client.Update(ctx, rp); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	injectedConfigVersion := ""
	if rp.Annotations != nil && rp.Annotations[RestartClusterOnConfigChangeKey] == "true" {
		injectedConfigVersion = rp.Status.ConfigVersion
	}
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, cluster, injectedConfigVersion)
	if err != nil {
		logger.Error(err, "fetching pools")
		return ctrl.Result{}, err
	}

	status := lifecycle.NewClusterStatus()
	status.Pools = pools.PoolStatuses()

	if pools.AnyReady() {
		status.Status.SetReady(statuses.ClusterReadyReasonReady)
	} else {
		status.Status.SetReady(statuses.ClusterReadyReasonNotReady, "No pods are ready")
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, cluster); deleted || err != nil {
			return r.syncStatusErr(ctx, err, status, cluster)
		}
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := r.Client.Update(ctx, rp); err != nil {
				logger.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// we are not deleting, so add a finalizer first before
	// allocating any additional resources
	if controllerutil.AddFinalizer(rp, FinalizerKey) {
		if err := r.Client.Update(ctx, rp); err != nil {
			logger.Error(err, "updating cluster finalizer")
			return ignoreConflict(err)
		}
		return ctrl.Result{}, nil
	}

	if err := validateClusterParameters(rp); err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())

		logger.Error(err, "validating cluster parameters")
		r.EventRecorder.Eventf(rp, "Warning", redpandav1alpha2.EventSeverityError, err.Error())
		return r.syncStatus(ctx, status, cluster)
	}

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.reconcileResources(ctx, cluster); err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())

		logger.Error(err, "error reconciling resources")
		return r.syncStatusErr(ctx, err, status, cluster)
	}

	// next we sync up all of our pools themselves
	requeue, err := r.reconcilePools(ctx, cluster, pools)
	if err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())

		logger.Error(err, "error reconciling pools")
		return r.syncStatusErr(ctx, err, status, cluster)
	}
	if requeue || !pools.AnyReady() {
		// we might have no brokers ready at this point, so we can't do anything below this
		// since they require the ability to talk to our brokers
		return r.syncStatusAndRequeue(ctx, status, cluster)
	}

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		logger.Error(err, "error fetching redpanda admin client")
		return r.syncStatusErr(ctx, err, status, cluster)
	}
	defer admin.Close()

	// now we ensure that we reconcile all of our decommissioning nodes
	// TODO: Do we want to rate limit this as well given that it also calls the admin API?
	// My thought is no since we want to be snappy with decommissioning.
	health, requeue, err := r.reconcileDecommission(ctx, cluster, admin, pools)
	if err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())
		if err != nil {
			if internalclient.IsTerminalClientError(err) {
				status.Status.SetHealthy(statuses.ClusterHealthyReasonTerminalError, err.Error())
			} else {
				status.Status.SetHealthy(statuses.ClusterHealthyReasonError, err.Error())
			}
		}

		logger.Error(err, "error decommissioning brokers")
		return r.syncStatusErr(ctx, err, status, cluster)
	}
	if requeue {
		return r.syncStatusAndRequeue(ctx, status, cluster)
	}

	status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonSynced)
	if health.IsHealthy {
		status.Status.SetHealthy(statuses.ClusterHealthyReasonHealthy)
	} else {
		// TODO: give more specific message here
		status.Status.SetHealthy(statuses.ClusterHealthyReasonNotHealthy, "Cluster is not healthy")
	}

	// we rate limit setting cluster configuration to once a minute if it's already been applied and is up-to-date
	// NB: For this and the next block, we heavily rely on the likelihood of things getting requeued on a fairly regular
	// basis, but in some odd cases this could potentially be problematic. Take for instance someone changing a ConfigMap
	// that gets materialized into the cluster configuration twice in somewhat rapid succession. Let's say that between changes
	// reconciliation gets run and configuration is synced. After the second change, if reconciliation is triggered again, then
	// the underlying configuration will not wind up getting updated because:
	//   1. the generation of the RP cluster hasn't changed
	//   2. we have set the given condition within the last minute
	// This could cause issues of staleness where we have to wait for retrigger of reconciliation either via some watched resource
	// change, or, in worst case, the default runtime cache-sync interval of ~10 hours. On the flip-side, it causes us to hammer
	// the API less often.
	if !statuses.HasRecentCondition(rp, statuses.ClusterConfigurationApplied, metav1.ConditionTrue, time.Minute) {
		version, requeue, err := r.reconcileClusterConfig(ctx, admin, rp)
		if err != nil {
			status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonError, err.Error())

			logger.Error(err, "error reconciling cluster config")
			return r.syncStatusErr(ctx, err, status, cluster)
		}

		status.ConfigVersion = ptr.To(version)

		if requeue {
			return r.syncStatusAndRequeue(ctx, status, cluster)
		}
	}

	status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)

	// rate limit license reconciliation
	if !statuses.HasRecentCondition(rp, statuses.ClusterLicenseValid, metav1.ConditionTrue, time.Minute) {
		license, err := r.reconcileLicense(ctx, admin, rp, status)
		if err != nil {
			logger.Error(err, "error reconciling license")
			return r.syncStatusErr(ctx, err, status, cluster)
		}
		if license != nil {
			rp.Status.LicenseStatus = license
		}
	} else {
		// just copy over the license status from our existing status
		status.Status.SetLicenseValidFromCurrent(rp)
	}

	return r.syncStatus(ctx, status, cluster)
}

func (r *RedpandaReconciler) reconcileResources(ctx context.Context, cluster *lifecycle.ClusterWithPools) (err error) {
	ctx, span := trace.Start(ctx, "reconcileResources")
	defer func() { trace.EndSpan(span, err) }()
	return r.LifecycleClient.SyncAll(ctx, cluster)
}

// reconcilePools is the meat of the controller. It handles creation and scale up/scale down
// of the given cluster pools. All scale up and update routines can happen concurrently, but
// every scale down happens a single broker at a time, ending reconciliation early and requeueing
// the cluster if a decommissioning operation/scale down is currently in progress.
func (r *RedpandaReconciler) reconcilePools(ctx context.Context, cluster *lifecycle.ClusterWithPools, pools *lifecycle.PoolTracker) (_ bool, err error) {
	ctx, span := trace.Start(ctx, "reconcilePools")
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	if !pools.CheckScale() {
		logger.V(log.TraceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return true, nil
	}

	logger.V(log.TraceLevel).Info("ready to scale and apply node pools", "existing", pools.ExistingStatefulSets(), "desired", pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range pools.ToCreate() {
		logger.V(log.TraceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return false, errors.Wrap(err, "creating statefulset")
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range pools.ToScaleUp() {
		logger.V(log.TraceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return false, errors.Wrap(err, "scaling up statefulset")
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range pools.RequiresUpdate() {
		logger.V(log.TraceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return false, errors.Wrap(err, "updating statefulset")
		}
	}

	return false, nil
}

func (r *RedpandaReconciler) reconcileDecommission(ctx context.Context, cluster *lifecycle.ClusterWithPools, admin *rpadmin.AdminAPI, pools *lifecycle.PoolTracker) (_ rpadmin.ClusterHealthOverview, _ bool, err error) {
	ctx, span := trace.Start(ctx, "reconcileDecommission")
	defer func() { trace.EndSpan(span, err) }()

	logger := log.FromContext(ctx)

	health, err := r.fetchClusterHealth(ctx, admin)
	if err != nil {
		return health, false, errors.Wrap(err, "fetching cluster health")
	}

	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := admin.Broker(ctx, brokerID)
		if err != nil {
			return health, false, errors.Wrap(err, "fetching broker")
		}

		brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
		brokerMap[brokerTokens[0]] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, admin, cluster, set, brokerMap)
		return health, requeue, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range pools.ToDelete() {
		logger.V(log.TraceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.Client.Delete(ctx, set); err != nil {
			return health, false, errors.Wrap(err, "deleting statefulset")
		}
	}

	// finally, we make sure we roll every pod that is not in-sync with its statefulset
	rollSet := pools.PodsToRoll()
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
			logger.V(log.TraceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := r.Client.Delete(ctx, pod); err != nil {
				return health, false, errors.Wrap(err, "deleting pod")
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			return health, true, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		return health, true, nil
	}

	return health, false, nil
}

func (r *RedpandaReconciler) setupLicense(ctx context.Context, rp *redpandav1alpha2.Redpanda, adminClient *rpadmin.AdminAPI) error {
	if rp.Spec.ClusterSpec == nil || rp.Spec.ClusterSpec.Enterprise == nil {
		return nil
	}

	if literalLicense := ptr.Deref(rp.Spec.ClusterSpec.Enterprise.License, ""); literalLicense != "" {
		if err := adminClient.SetLicense(ctx, strings.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	if secretReference := rp.Spec.ClusterSpec.Enterprise.LicenseSecretRef; secretReference != nil {
		var licenseSecret corev1.Secret

		name := ptr.Deref(secretReference.Name, "")
		key := ptr.Deref(secretReference.Key, "")
		if name == "" || key == "" {
			return errors.Newf("both name %q and key %q of the secret can not be empty string", name, key)
		}

		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: rp.Namespace, Name: name}, &licenseSecret); err != nil {
			return errors.WithStack(err)
		}

		literalLicense := licenseSecret.Data[key]
		if err := adminClient.SetLicense(ctx, bytes.NewReader(literalLicense)); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (r *RedpandaReconciler) reconcileLicense(ctx context.Context, admin *rpadmin.AdminAPI, rp *redpandav1alpha2.Redpanda, status *lifecycle.ClusterStatus) (_ *redpandav1alpha2.RedpandaLicenseStatus, err error) {
	ctx, span := trace.Start(ctx, "reconcileLicense")
	defer func() { trace.EndSpan(span, err) }()

	if err := r.setupLicense(ctx, rp, admin); err != nil {
		return nil, errors.WithStack(err)
	}

	features, err := admin.GetEnterpriseFeatures(ctx)
	if err != nil {
		if internalclient.IsTerminalClientError(err) {
			status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonTerminalError, err.Error())
		} else {
			status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonError, err.Error())
		}

		return nil, errors.WithStack(err)
	}

	licenseInfo, err := admin.GetLicenseInfo(ctx)
	if err != nil {
		if internalclient.IsTerminalClientError(err) {
			status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonTerminalError, err.Error())
		} else {
			status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonError, err.Error())
		}

		return nil, errors.WithStack(err)
	}

	switch features.LicenseStatus {
	case rpadmin.LicenseStatusExpired:
		status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonExpired)
	case rpadmin.LicenseStatusNotPresent:
		status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonNotPresent)
	case rpadmin.LicenseStatusValid:
		status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonValid)
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

	return licenseStatus(), nil
}

func (r *RedpandaReconciler) reconcileClusterConfig(ctx context.Context, admin *rpadmin.AdminAPI, rp *redpandav1alpha2.Redpanda) (_ string, _ bool, err error) {
	ctx, span := trace.Start(ctx, "reconcileClusterConfig")
	defer func() { trace.EndSpan(span, err) }()

	schema, err := admin.ClusterConfigSchema(ctx)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	config, err := r.clusterConfigFor(ctx, rp, schema)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	usersTXT, err := r.usersTXTFor(ctx, rp)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	mode := r.configSyncMode(ctx, rp)

	syncer := syncclusterconfig.Syncer{Client: admin, Mode: mode}
	configStatus, err := syncer.Sync(ctx, config, usersTXT)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	// TODO: this needs to be updated when the hashing code lands
	return strconv.FormatInt(configStatus.Version, 10), configStatus.NeedsRestart, nil
}

func (r *RedpandaReconciler) configSyncMode(ctx context.Context, rp *redpandav1alpha2.Redpanda) syncclusterconfig.SyncerMode {
	switch strings.ToLower(rp.Annotations[SyncerModeKey]) {
	case SyncerModeDeclarative:
		return syncclusterconfig.SyncerModeDeclarative
	case "", SyncerModeAdditive:
		return syncclusterconfig.SyncerModeAdditive
	default:
		logger := log.FromContext(ctx)
		logger.Info("unrecognised value %q for %s, syncing config in Additive mode", rp.Annotations[SyncerModeKey], SyncerModeKey)
		return syncclusterconfig.SyncerModeAdditive
	}
}

func (r *RedpandaReconciler) usersTXTFor(ctx context.Context, rp *redpandav1alpha2.Redpanda) (map[string][]byte, error) {
	values, err := rp.GetValues()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if !values.Auth.SASL.Enabled {
		return map[string][]byte{}, nil
	}

	key := client.ObjectKey{Namespace: rp.Namespace, Name: values.Auth.SASL.SecretRef}

	var users corev1.Secret
	if err := r.Client.Get(ctx, key, &users); err != nil {
		if apierrors.IsNotFound(err) {
			return map[string][]byte{}, nil
		}
		return nil, errors.WithStack(err)
	}

	return users.Data, nil
}

func (r *RedpandaReconciler) clusterConfigFor(ctx context.Context, rp *redpandav1alpha2.Redpanda, schema rpadmin.ConfigSchema) (_ map[string]any, err error) {
	// Parinoided panic catch as we're calling directly into helm functions.
	defer func() {
		if r := recover(); r != nil {
			err = errors.Newf("recovered panic: %+v", r)
		}
	}()

	dot, err := rp.GetDot(r.KubeConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// The most reliable way to get the correct and full cluster config is to
	// "envsubst" the bootstrap file itself as various components feed into the
	// final cluster config and they may be referencing values stored in
	// configmaps or secrets.
	job := redpanda.PostInstallUpgradeJob(dot)
	clusterConfigTemplate, fixups := redpanda.BootstrapContents(dot)
	conf := clusterconfiguration.NewClusterCfg(clusterconfiguration.NewPodContext(rp.Namespace))
	for k, v := range clusterConfigTemplate {
		conf.SetAdditionalConfiguration(k, v)
	}
	for _, f := range fixups {
		conf.AddFixup(f.Field, f.CEL)
	}
	for _, e := range job.Spec.Template.Spec.InitContainers[0].Env {
		if err := conf.EnsureInitEnv(e); err != nil {
			return nil, errors.WithStack(err)
		}
	}
	for _, e := range job.Spec.Template.Spec.InitContainers[0].EnvFrom {
		if err := conf.EnsureInitEnvFrom(e); err != nil {
			return nil, errors.WithStack(err)
		}
	}

	desired, err := conf.Reify(ctx, r.Client, r.CloudSecretsExpander, schema)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return desired, nil
}

// syncStatus updates the status of the Redpanda cluster at the end of reconciliation when
// no more reconciliation should occur.
func (r *RedpandaReconciler) syncStatus(ctx context.Context, status *lifecycle.ClusterStatus, cluster *lifecycle.ClusterWithPools) (ctrl.Result, error) {
	return r.syncStatusErr(ctx, nil, status, cluster)
}

// syncStatus updates the status of the Redpanda cluster when an error has occurred in the
// reconciliation process and the current loop should be aborted but retried.
func (r *RedpandaReconciler) syncStatusErr(ctx context.Context, err error, status *lifecycle.ClusterStatus, cluster *lifecycle.ClusterWithPools) (ctrl.Result, error) {
	if r.LifecycleClient.SetClusterStatus(cluster, status) {
		syncErr := r.Client.Status().Update(ctx, cluster.Redpanda)
		err = errors.Join(syncErr, err)
	}

	return ignoreConflict(err)
}

func (r *RedpandaReconciler) syncStatusAndRequeue(ctx context.Context, status *lifecycle.ClusterStatus, cluster *lifecycle.ClusterWithPools) (ctrl.Result, error) {
	var err error
	if r.LifecycleClient.SetClusterStatus(cluster, status) {
		err = r.Client.Status().Update(ctx, cluster.Redpanda)
	}

	result, err := ignoreConflict(err)
	result.Requeue = true
	result.RequeueAfter = requeueTimeout

	return result, err
}

func (r *RedpandaReconciler) fetchClusterHealth(ctx context.Context, admin *rpadmin.AdminAPI) (_ rpadmin.ClusterHealthOverview, err error) {
	ctx, span := trace.Start(ctx, "reconcileResources")
	defer func() { trace.EndSpan(span, err) }()

	var health rpadmin.ClusterHealthOverview

	health, err = admin.GetHealthOverview(ctx)
	if err != nil {
		return health, err
	}

	return health, nil
}

// scaleDown contains the majority of the logic for scaling down a statefulset incrementally, first
// decommissioning the broker with the last pod ordinal and then patching the statefulset with
// a single less replica.
func (r *RedpandaReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.ClusterWithPools, set *lifecycle.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].scaleDown", *cluster))
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
func (r *RedpandaReconciler) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster *lifecycle.ClusterWithPools, set *lifecycle.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(log.TraceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(log.TraceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, errors.Wrap(err, "decommissioning broker")
			}

			return true, nil
		} else {
			return false, errors.Wrap(err, "fetching decommission status")
		}
	}
	if !decommissionStatus.Finished {
		logger.V(log.TraceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

		// just requeue since we're still decommissioning
		return true, nil
	}

	// we're finished
	return false, nil
}

// ignoreConflict ignores errors that happen due to optimistic locking
// checks. This is safe because it means that the client-side cache
// hasn't yet received the update of the resource from the server, and
// once it does reconciliation will be retriggered. To be safe, we
// also explicitly trigger a requeue.
func ignoreConflict(err error) (ctrl.Result, error) {
	if apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}

func validateClusterParameters(rp *redpandav1alpha2.Redpanda) error {
	// Upgrade checks. Don't reconcile if UseFlux is true or if ChartRef is set.
	if rp.Spec.ChartRef.UseFlux != nil && *rp.Spec.ChartRef.UseFlux {
		return errors.New("useFlux: true is no longer supported. Please downgrade or unset `useFlux`")
	}

	if rp.Spec.ChartRef.ChartVersion != "" {
		return errors.New("Specifying chartVersion is no longer supported. Please downgrade or unset `chartRef.chartVersion`")
	}

	return nil
}

func isRedpandaManaged(ctx context.Context, redpandaCluster *redpandav1alpha2.Redpanda) bool {
	logger := log.FromContext(ctx)

	managedAnnotationKey := redpandav1alpha2.GroupVersion.Group + managedPath
	if managed, exists := redpandaCluster.Annotations[managedAnnotationKey]; exists && managed == NotManaged {
		logger.V(log.TraceLevel).Info(fmt.Sprintf("management is disabled; to enable it, change the '%s' annotation to true or remove it", managedAnnotationKey))
		return false
	}
	return true
}
