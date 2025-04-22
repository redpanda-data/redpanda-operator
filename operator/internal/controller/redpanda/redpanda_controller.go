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
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

const (
	FinalizerKey                    = "operator.redpanda.com/finalizer"
	ClusterConfigNeedRestartHashKey = "operator.redpanda.com/cluster-config-need-restart-hash"
	RestartClusterOnConfigChangeKey = "operator.redpanda.com/restart-cluster-on-config-change"
	FluxFinalizerKey                = "finalizers.fluxcd.io"

	NotManaged = "false"

	managedPath = "/managed"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"

	// reqeueueTimeout is the time that the reconciler will
	// wait before requeueing a cluster to be reconciled when
	// we've explicitly aborted a reconciliation loop early
	// due to an in-progress oepration
	requeueTimeout = 10 * time.Second
)

type Image struct {
	Repository string
	Tag        string
}

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	// KubeConfig is the [rest.Config] that provides the go helm chart
	// Kubernetes access. It should be the same config used to create client.
	KubeConfig      *rest.Config
	Client          client.Client
	LifecycleClient *lifecycle.ResourceClient[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda]
	EventRecorder   kuberecorder.EventRecorder
	ClientFactory   internalclient.ClientFactory
	// OperatorImage is the image to use for any instances of the operator
	// within redpanda deployments. e.g. StatefulSet.Sidecar.Image. The
	// redpanda chart ships with it's own default but we want this field to be
	// controlled by the operator.
	OperatorImage Image
}

// Any resource that the Redpanda helm chart creates and needs to reconcile.
// +kubebuilder:rbac:groups="",namespace=default,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=default,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,namespace=default,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete;
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

	r.LifecycleClient.WatchResources(builder, &redpandav1alpha2.Redpanda{})

	return builder.Complete(r)
}

func (r *RedpandaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.Reconcile")

	defer func() {
		durationMsg := fmt.Sprintf("reconciliation finished in %s", time.Since(start).String())
		log.V(TraceLevel).Info(durationMsg)
	}()

	log.V(TraceLevel).Info("Starting reconcile loop")

	rp := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// grab our existing and desired pool resources
	// so that we can immediately calculate cluster status
	// from and sync in any subsequent operation that
	// early returns
	pools, err := r.LifecycleClient.FetchExistingAndDesiredPools(ctx, rp)
	if err != nil {
		log.Error(err, "fetching pools")
		return ctrl.Result{}, err
	}

	status := lifecycle.NewClusterStatus()
	status.Pools = pools.PoolStatuses()

	if !isRedpandaManaged(ctx, rp) {
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := r.Client.Update(ctx, rp); err != nil {
				log.Error(err, "updating cluster finalizer")
				// no need to update the status at this point since the
				// previous update failed
				return ignoreConflict(err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		// clean up all dependant resources
		if deleted, err := r.LifecycleClient.DeleteAll(ctx, rp); deleted || err != nil {
			return r.syncStatusErr(ctx, err, status, rp)
		}
		if controllerutil.RemoveFinalizer(rp, FinalizerKey) {
			if err := r.Client.Update(ctx, rp); err != nil {
				log.Error(err, "updating cluster finalizer")
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
			log.Error(err, "updating cluster finalizer")
			return ignoreConflict(err)
		}
		return ctrl.Result{}, nil
	}

	// TODO: can we remove this sooner than later as it's essentially advisory?
	if err := validateClusterParameters(rp); err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonTerminalError, err.Error())

		log.Error(err, "validating cluster parameters")
		r.EventRecorder.Eventf(rp, "Warning", redpandav1alpha2.EventSeverityError, err.Error())
		return r.syncStatus(ctx, status, rp)
	}

	// we sync all our non pool resources first so that they're in-place
	// prior to us scaling up our node pools
	if err := r.reconcileResources(ctx, rp); err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())

		log.Error(err, "error reconciling resources")
		return r.syncStatusErr(ctx, err, status, rp)
	}

	// next we sync up all of our pools themselves
	admin, requeue, err := r.reconcilePools(ctx, rp, pools)
	if err != nil {
		status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonError, err.Error())

		log.Error(err, "error reconciling pools")
		return r.syncStatusErr(ctx, err, status, rp)
	}
	if requeue {
		return r.syncStatusAndRequeue(ctx, status, rp)
	}
	defer admin.Close()

	status.Status.SetResourcesSynced(statuses.ClusterResourcesSyncedReasonSynced)

	_, requeue, err = r.reconcileClusterConfig(ctx, admin, rp)
	if err != nil {
		status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonError, err.Error())

		log.Error(err, "error reconciling cluster config")
		return r.syncStatusErr(ctx, err, status, rp)
	}
	if requeue {
		return r.syncStatusAndRequeue(ctx, status, rp)
	}

	status.Status.SetConfigurationApplied(statuses.ClusterConfigurationAppliedReasonApplied)

	license, err := r.reconcileLicense(ctx, admin, rp, status)
	if err != nil {
		log.Error(err, "error reconciling license")
		return r.syncStatusErr(ctx, err, status, rp)
	}
	if license != nil {
		rp.Status.LicenseStatus = license
	}

	if err := r.reconcileClusterHealth(ctx, admin, status); err != nil {
		log.Error(err, "error reconciling cluster healthy")
		return r.syncStatusErr(ctx, err, status, rp)
	}

	return r.syncStatus(ctx, status, rp)
}

func (r *RedpandaReconciler) reconcileResources(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	cloned := rp.DeepCopy()
	if cloned.Spec.ClusterSpec == nil {
		cloned.Spec.ClusterSpec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	if cloned.Spec.ClusterSpec.Statefulset == nil {
		cloned.Spec.ClusterSpec.Statefulset = &redpandav1alpha2.Statefulset{}
	}

	if cloned.Spec.ClusterSpec.Statefulset.SideCars == nil {
		cloned.Spec.ClusterSpec.Statefulset.SideCars = &redpandav1alpha2.SideCars{}
	}

	if cloned.Spec.ClusterSpec.Statefulset.SideCars.Image == nil {
		cloned.Spec.ClusterSpec.Statefulset.SideCars.Image = &redpandav1alpha2.RedpandaImage{}
	}

	// If not explicitly specified, set the tag and repository of the sidecar
	// to the image specified via CLI args rather than relying on the default
	// of the redpanda chart.
	// This ensures that custom deployments (e.g.
	// localhost/redpanda-operator:dev) will use the image they are deployed
	// with.
	if cloned.Spec.ClusterSpec.Statefulset.SideCars.Image.Tag == nil {
		cloned.Spec.ClusterSpec.Statefulset.SideCars.Image.Tag = &r.OperatorImage.Tag
	}

	if cloned.Spec.ClusterSpec.Statefulset.SideCars.Image.Repository == nil {
		cloned.Spec.ClusterSpec.Statefulset.SideCars.Image.Repository = &r.OperatorImage.Repository
	}

	return r.LifecycleClient.SyncAll(ctx, cloned)
}

// reconcilePools is the meat of the controller. It handles creation and scale up/scale down
// of the given cluster pools. All scale up and update routines can happen concurrently, but
// every scale down happens a single broker at a time, ending reconciliation early and requeueing
// the cluster if a decommissioning operation/scale down is currently in progress.
func (r *RedpandaReconciler) reconcilePools(ctx context.Context, cluster *redpandav1alpha2.Redpanda, pools *lifecycle.PoolTracker) (*rpadmin.AdminAPI, bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].reconcilePools", *cluster))

	if !pools.CheckScale() {
		logger.V(TraceLevel).Info("scale operation currently underway")
		// we're not yet ready to scale, so just requeue
		return nil, true, nil
	}

	logger.V(TraceLevel).Info("ready to scale and apply node pools", "existing", pools.ExistingStatefulSets(), "desired", pools.DesiredStatefulSets())

	// first create any pools that don't currently exists
	for _, set := range pools.ToCreate() {
		logger.V(TraceLevel).Info("creating StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("creating statefulset: %w", err)
		}
	}

	// next scale up any under-provisioned pools and patch them to use the new spec
	for _, set := range pools.ToScaleUp() {
		logger.V(TraceLevel).Info("scaling up StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())

		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("scaling up statefulset: %w", err)
		}
	}

	// now make sure all of the patch any sets that might have changed without affecting the cluster size
	// here we can just wholesale patch everything
	for _, set := range pools.RequiresUpdate() {
		logger.V(TraceLevel).Info("updating out-of-date StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set); err != nil {
			return nil, false, fmt.Errorf("updating statefulset: %w", err)
		}
	}

	admin, health, err := r.fetchClusterHealth(ctx, cluster)
	if err != nil {
		return nil, false, fmt.Errorf("fetching cluster health: %w", err)
	}

	brokerMap := map[string]int{}
	for _, brokerID := range health.AllNodes {
		broker, err := admin.Broker(ctx, brokerID)
		if err != nil {
			admin.Close()
			return nil, false, fmt.Errorf("fetching broker: %w", err)
		}

		brokerTokens := strings.Split(broker.InternalRPCAddress, ".")
		brokerMap[brokerTokens[0]] = brokerID
	}

	// next scale down any over-provisioned pools, patching them to use the new spec
	// and decommissioning any nodes as needed
	for _, set := range pools.ToScaleDown() {
		requeue, err := r.scaleDown(ctx, admin, cluster, set, brokerMap)
		admin.Close()
		return nil, requeue, err
	}

	// at this point any set that needs to be deleted should have 0 replicas
	// so we can attempt to delete them all in one pass
	for _, set := range pools.ToDelete() {
		logger.V(TraceLevel).Info("deleting StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set).String())
		if err := r.Client.Delete(ctx, set); err != nil {
			admin.Close()
			return nil, false, fmt.Errorf("deleting statefulset: %w", err)
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
			logger.V(TraceLevel).Info("rolling pod", "Pod", client.ObjectKeyFromObject(pod).String())

			if err := r.Client.Delete(ctx, pod); err != nil {
				admin.Close()
				return nil, false, fmt.Errorf("deleting pod: %w", err)
			}
		}

		if !continueExecution {
			// requeue since we just rolled a pod
			// and we want for the system to stabilize
			admin.Close()
			return nil, true, nil
		}
	}

	if !rolled && len(rollSet) > 0 {
		// here we're in a state where we can't currently roll any
		// pods but we need to, therefore we just reschedule rather
		// than marking the cluster as quiesced.
		admin.Close()
		return nil, true, nil
	}

	return admin, false, nil
}

func (r *RedpandaReconciler) setupLicense(ctx context.Context, rp *redpandav1alpha2.Redpanda, adminClient *rpadmin.AdminAPI) error {
	if rp.Spec.ClusterSpec.Enterprise == nil {
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

func (r *RedpandaReconciler) reconcileLicense(ctx context.Context, admin *rpadmin.AdminAPI, rp *redpandav1alpha2.Redpanda, status *lifecycle.ClusterStatus) (*redpandav1alpha2.RedpandaLicenseStatus, error) {
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
		status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonExpired, "Expired")
	case rpadmin.LicenseStatusNotPresent:
		status.Status.SetLicenseValid(statuses.ClusterLicenseValidReasonNotPresent, "Not Present")
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

func (r *RedpandaReconciler) reconcileClusterHealth(ctx context.Context, admin *rpadmin.AdminAPI, status *lifecycle.ClusterStatus) error {
	overview, err := admin.GetHealthOverview(ctx)
	if err != nil {
		if internalclient.IsTerminalClientError(err) {
			status.Status.SetHealthy(statuses.ClusterHealthyReasonTerminalError, err.Error())
		} else {
			status.Status.SetHealthy(statuses.ClusterHealthyReasonError, err.Error())
		}

		return errors.WithStack(err)
	}

	if overview.IsHealthy {
		status.Status.SetHealthy(statuses.ClusterHealthyReasonHealthy)
	} else {
		// TODO: give more specific message here
		status.Status.SetHealthy(statuses.ClusterHealthyReasonNotHealthy, "Cluster is not healthy")
	}

	return nil
}

func (r *RedpandaReconciler) reconcileClusterConfig(ctx context.Context, admin *rpadmin.AdminAPI, rp *redpandav1alpha2.Redpanda) (string, bool, error) {
	config, err := r.clusterConfigFor(ctx, rp)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	usersTXT, err := r.usersTXTFor(ctx, rp)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	syncer := syncclusterconfig.Syncer{Client: admin, Mode: syncclusterconfig.SyncerModeAdditive}
	configStatus, err := syncer.Sync(ctx, config, usersTXT)
	if err != nil {
		return "", false, errors.WithStack(err)
	}

	hash := ""
	return hash, configStatus.NeedsRestart, nil
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

func (r *RedpandaReconciler) clusterConfigFor(ctx context.Context, rp *redpandav1alpha2.Redpanda) (_ map[string]any, err error) {
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
	clusterConfigTemplate := redpanda.BootstrapFile(dot)

	expander := kube.EnvExpander{
		Client:    r.Client,
		Namespace: rp.Namespace,
		Env:       job.Spec.Template.Spec.InitContainers[0].Env,
		EnvFrom:   job.Spec.Template.Spec.InitContainers[0].EnvFrom,
	}

	expanded, err := expander.Expand(ctx, clusterConfigTemplate)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var desired map[string]any
	if err := yaml.Unmarshal([]byte(expanded), &desired); err != nil {
		return nil, errors.WithStack(err)
	}

	return desired, nil
}

func (r *RedpandaReconciler) needsDecommission(ctx context.Context, rp *redpandav1alpha2.Redpanda, stses []*appsv1.StatefulSet) (bool, error) {
	client, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return false, errors.WithStack(err)
	}
	defer client.Close()

	health, err := client.GetHealthOverview(ctx)
	if err != nil {
		return false, errors.WithStack(err)
	}

	desiredReplicas := 0
	for _, sts := range stses {
		desiredReplicas += int(ptr.Deref(sts.Spec.Replicas, 0))
	}

	if len(health.AllNodes) == 0 || desiredReplicas == 0 {
		return false, nil
	}

	return len(health.AllNodes) > desiredReplicas, nil
}

// syncStatus updates the status of the Redpanda cluster at the end of reconciliation when
// no more reconciliation should occur.
func (r *RedpandaReconciler) syncStatus(ctx context.Context, status *lifecycle.ClusterStatus, cluster *redpandav1alpha2.Redpanda) (ctrl.Result, error) {
	return r.syncStatusErr(ctx, nil, status, cluster)
}

// syncStatus updates the status of the Redpanda cluster when an error has occurred in the
// reconciliation process and the current loop should be aborted but retried.
func (r *RedpandaReconciler) syncStatusErr(ctx context.Context, err error, status *lifecycle.ClusterStatus, cluster *redpandav1alpha2.Redpanda) (ctrl.Result, error) {
	if r.LifecycleClient.SetClusterStatus(cluster, status) {
		syncErr := r.Client.Status().Update(ctx, cluster)
		err = errors.Join(syncErr, err)
	}

	return ignoreConflict(err)
}

func (r *RedpandaReconciler) syncStatusAndRequeue(ctx context.Context, status *lifecycle.ClusterStatus, cluster *redpandav1alpha2.Redpanda) (ctrl.Result, error) {
	var err error
	if r.LifecycleClient.SetClusterStatus(cluster, status) {
		err = r.Client.Status().Update(ctx, cluster)
	}

	result, err := ignoreConflict(err)
	result.Requeue = true
	result.RequeueAfter = requeueTimeout

	return result, err
}

func (r *RedpandaReconciler) fetchClusterHealth(ctx context.Context, cluster *redpandav1alpha2.Redpanda) (*rpadmin.AdminAPI, rpadmin.ClusterHealthOverview, error) {
	var health rpadmin.ClusterHealthOverview

	admin, err := r.ClientFactory.RedpandaAdminClient(ctx, cluster)
	if err != nil {
		return nil, health, err
	}
	health, err = admin.GetHealthOverview(ctx)
	if err != nil {
		return nil, health, err
	}

	return admin, health, nil
}

// scaleDown contains the majority of the logic for scaling down a statefulset incrementally, first
// decommissioning the broker with the last pod ordinal and then patching the statefulset with
// a single less replica.
func (r *RedpandaReconciler) scaleDown(ctx context.Context, admin *rpadmin.AdminAPI, cluster *redpandav1alpha2.Redpanda, set *lifecycle.ScaleDownSet, brokerMap map[string]int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].scaleDown", *cluster))
	logger.V(TraceLevel).Info("starting StatefulSet scale down", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

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

	logger.V(TraceLevel).Info("scaling down StatefulSet", "StatefulSet", client.ObjectKeyFromObject(set.StatefulSet).String())

	// now patch the statefulset to remove the pod
	if err := r.LifecycleClient.PatchNodePoolSet(ctx, cluster, set.StatefulSet); err != nil {
		return false, fmt.Errorf("scaling down statefulset: %w", err)
	}
	// we only do a statefulset at a time, waiting for them to
	// become stable first
	return true, nil
}

// decommissionBroker handles decommissioning a broker and waiting until it has finished decommissioning
func (r *RedpandaReconciler) decommissionBroker(ctx context.Context, admin *rpadmin.AdminAPI, cluster *redpandav1alpha2.Redpanda, set *lifecycle.ScaleDownSet, brokerID int) (bool, error) {
	logger := log.FromContext(ctx).WithName(fmt.Sprintf("ClusterReconciler[%T].decommissionBroker", *cluster))
	logger.V(TraceLevel).Info("checking decommissioning status for pod", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

	decommissionStatus, err := admin.DecommissionBrokerStatus(ctx, brokerID)
	if err != nil {
		if strings.Contains(err.Error(), "is not decommissioning") {
			logger.V(TraceLevel).Info("decommissioning broker", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

			if err := admin.DecommissionBroker(ctx, brokerID); err != nil {
				return false, fmt.Errorf("decommissioning broker: %w", err)
			}

			return true, nil
		} else {
			return false, fmt.Errorf("fetching decommission status: %w", err)
		}
	}
	if !decommissionStatus.Finished {
		logger.V(TraceLevel).Info("decommissioning in progress", "Pod", client.ObjectKeyFromObject(set.LastPod).String())

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
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.isRedpandaManaged")

	managedAnnotationKey := redpandav1alpha2.GroupVersion.Group + managedPath
	if managed, exists := redpandaCluster.Annotations[managedAnnotationKey]; exists && managed == NotManaged {
		log.Info(fmt.Sprintf("management is disabled; to enable it, change the '%s' annotation to true or remove it", managedAnnotationKey))
		return false
	}
	return true
}
