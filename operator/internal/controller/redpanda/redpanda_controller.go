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
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/redpanda-data/common-go/rpadmin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

const (
	FinalizerKey = "operator.redpanda.com/finalizer"

	NotManaged = "false"

	managedPath = "/managed"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"
)

type gvkKey struct {
	GVK schema.GroupVersionKind
	Key client.ObjectKey
}

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	// KubeConfig is the [kube.Config] that provides the go helm chart
	// Kubernetes access. It should be the same config used to create client.
	KubeConfig    kube.Config
	Client        client.Client
	Scheme        *runtime.Scheme
	EventRecorder kuberecorder.EventRecorder
	ClientFactory internalclient.ClientFactory
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

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := registerHelmReferencedIndex(ctx, mgr, "statefulset", &appsv1.StatefulSet{}); err != nil {
		return err
	}
	if err := registerHelmReferencedIndex(ctx, mgr, "deployment", &appsv1.Deployment{}); err != nil {
		return err
	}

	helmManagedComponentPredicate, err := predicate.LabelSelectorPredicate(
		metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "app.kubernetes.io/name", // look for only redpanda or console pods
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"redpanda", "console", "connectors"},
			}, {
				Key:      "app.kubernetes.io/instance", // make sure we have a cluster name
				Operator: metav1.LabelSelectorOpExists,
			}, {
				Key:      "batch.kubernetes.io/job-name", // filter out the job pods since they also have name=redpanda
				Operator: metav1.LabelSelectorOpDoesNotExist,
			}},
		},
	)
	if err != nil {
		return err
	}

	managedWatchOption := builder.WithPredicates(helmManagedComponentPredicate)

	return ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Redpanda{}).
		Watches(&appsv1.StatefulSet{}, enqueueClusterFromHelmManagedObject(), managedWatchOption).
		Watches(&appsv1.Deployment{}, enqueueClusterFromHelmManagedObject(), managedWatchOption).
		Complete(r)
}

func (r *RedpandaReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.Reconcile")

	defer func() {
		durationMsg := fmt.Sprintf("reconciliation finished in %s", time.Since(start).String())
		log.V(logger.TraceLevel).Info(durationMsg)
	}()

	log.V(logger.TraceLevel).Info("Starting reconcile loop")

	rp := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, rp)
	}

	if !isRedpandaManaged(ctx, rp) {
		if controllerutil.ContainsFinalizer(rp, FinalizerKey) {
			// if no longer managed by us, attempt to remove the finalizer
			controllerutil.RemoveFinalizer(rp, FinalizerKey)
			if err := r.Client.Update(ctx, rp); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	_, ok := rp.GetAnnotations()[resources.ManagedDecommissionAnnotation]
	if ok {
		log.V(logger.TraceLevel).Info("Managed decommission")
		return ctrl.Result{}, nil
	}

	// add finalizer if not exist
	if !controllerutil.ContainsFinalizer(rp, FinalizerKey) {
		patch := client.MergeFrom(rp.DeepCopy())
		controllerutil.AddFinalizer(rp, FinalizerKey)
		if err := r.Client.Patch(ctx, rp, patch); err != nil {
			log.Error(err, "unable to register finalizer")
			return ctrl.Result{}, err
		}
	}

	// Upgrade checks. Don't reconcile if UseFlux is true or if ChartRef is set.
	if rp.Spec.ChartRef.UseFlux != nil && *rp.Spec.ChartRef.UseFlux {
		log.Error(nil, "useFlux: true is no longer supported. Please downgrade or unset `useFlux`")
		return ctrl.Result{}, nil
	}

	if rp.Spec.ChartRef.ChartVersion != "" {
		msg := "Specifying chartVersion is no longer supported. Please downgrade or unset `chartRef.chartVersion`"
		log.Error(nil, msg)
		r.EventRecorder.Eventf(rp, "Warning", redpandav1alpha2.EventSeverityError, msg)
		return ctrl.Result{}, nil
	}

	if err := r.reconcileResources(ctx, rp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileStatus(ctx, rp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileLicense(ctx, rp); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileClusterConfig(ctx, rp); err != nil {
		return ctrl.Result{}, err
	}

	// Reconciliation has completed without any errors, therefore we observe our
	// generation and persist any status changes.
	rp.Status.ObservedGeneration = rp.Generation

	// Update status after reconciliation.
	if updateStatusErr := r.patchRedpandaStatus(ctx, rp); updateStatusErr != nil {
		log.Error(updateStatusErr, "unable to update status after reconciliation")
		return ctrl.Result{}, updateStatusErr
	}

	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileStatus(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	// pull our deployments and stateful sets
	redpandaStatefulSets, err := redpandaStatefulSetsForCluster(ctx, r.Client, rp)
	if err != nil {
		return err
	}

	consoleDeployments, err := consoleDeploymentsForCluster(ctx, r.Client, rp)
	if err != nil {
		return err
	}

	connectorsDeployments, err := connectorsDeploymentsForCluster(ctx, r.Client, rp)
	if err != nil {
		return err
	}

	deployments := append(consoleDeployments, connectorsDeployments...)

	if len(redpandaStatefulSets) == 0 {
		_ = redpandav1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", "Redpanda StatefulSet not yet created")
		return nil
	}

	// check to make sure that our stateful set pods are all current
	if message, ready := checkStatefulSetStatus(redpandaStatefulSets); !ready {
		_ = redpandav1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", message)
		return nil
	}

	// check to make sure that our deployment pods are all current
	if message, ready := checkDeploymentsStatus(deployments); !ready {
		_ = redpandav1alpha2.RedpandaNotReady(rp, "ConsolePodsNotReady", message)
		return nil
	}

	// Once we know that STS Pods are up and running, make sure that we don't
	// need to perform a decommission.
	needsDecommission, err := r.needsDecommission(ctx, rp, redpandaStatefulSets)
	if err != nil {
		return err
	}

	if needsDecommission {
		_ = redpandav1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", "Cluster currently decommissioning dead nodes")
		return nil
	}

	_ = redpandav1alpha2.RedpandaReady(rp)
	return nil
}

func (r *RedpandaReconciler) reconcileResources(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx)

	// DeepCopy values to prevent any accidental mutations that may occur
	// within the chart itself.
	values := rp.Spec.ClusterSpec.DeepCopy()

	objs, err := redpanda.Chart.Render(&r.KubeConfig, helmette.Release{
		Namespace: rp.Namespace,
		Name:      rp.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, values)
	if err != nil {
		return err
	}

	// set for tracking which objects are expected to exist in this reconciliation run.
	created := make(map[gvkKey]struct{}, len(objs))
	for _, obj := range objs {
		// Namespace is inconsistently set across all our charts. Set it
		// explicitly here to be safe.
		obj.SetNamespace(rp.Namespace)
		obj.SetOwnerReferences([]metav1.OwnerReference{rp.OwnerShipRefObj()})

		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		annos := obj.GetAnnotations()
		if annos == nil {
			annos = map[string]string{}
		}

		// Needed for interop with flux.
		// Without these the flux controller will refuse to take ownership.
		annos["meta.helm.sh/release-name"] = rp.GetHelmReleaseName()
		annos["meta.helm.sh/release-namespace"] = rp.Namespace

		labels["helm.toolkit.fluxcd.io/name"] = rp.GetHelmReleaseName()
		labels["helm.toolkit.fluxcd.io/namespace"] = rp.Namespace

		obj.SetLabels(labels)
		obj.SetAnnotations(annos)

		if _, ok := annos["helm.sh/hook"]; ok {
			log.V(logger.TraceLevel).Info(fmt.Sprintf("skipping helm hook %T: %q", obj, obj.GetName()))
			continue
		}

		// TODO: how to handle immutable issues?
		if err := r.apply(ctx, obj); err != nil {
			return errors.Wrapf(err, "deploying %T: %q", obj, obj.GetName())
		}

		log.V(logger.TraceLevel).Info(fmt.Sprintf("deployed %T: %q", obj, obj.GetName()))

		// Record creation
		created[gvkKey{
			Key: client.ObjectKeyFromObject(obj),
			GVK: obj.GetObjectKind().GroupVersionKind(),
		}] = struct{}{}
	}

	// If our ObservedGeneration is up to date, .Spec hasn't changed since the
	// last successful reconciliation so everything that we'd do here is likely
	// to be a no-op.
	// This check could likely be hoisted above the deployment loop as well.
	if rp.Generation == rp.Status.ObservedGeneration && rp.Generation != 0 {
		log.V(logger.TraceLevel).Info("observed generation is up to date. skipping garbage collection", "generation", rp.Generation, "observedGeneration", rp.Status.ObservedGeneration)
		return nil
	}

	// Garbage collect any objects that are no longer needed.
	if err := r.reconcileGC(ctx, rp, created); err != nil {
		return err
	}

	return nil
}

func (r *RedpandaReconciler) ratelimitCondition(ctx context.Context, rp *redpandav1alpha2.Redpanda, conditionType string) bool {
	log := ctrl.LoggerFrom(ctx)

	redpandaReady := apimeta.IsStatusConditionTrue(rp.Status.Conditions, redpandav1alpha2.ReadyCondition)

	if !(rp.GenerationObserved() || redpandaReady) {
		log.V(logger.TraceLevel).Info(fmt.Sprintf("redpanda not yet ready. skipping %s reconciliation.", conditionType))
		apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: rp.Generation,
			Reason:             "RedpandaNotReady",
			Message:            "redpanda cluster is not yet ready or up to date",
		})

		// NB: Redpanda becoming ready and/or observing it's generation will
		// trigger a re-queue for us.
		return true
	}

	cond := apimeta.FindStatusCondition(rp.Status.Conditions, conditionType)
	if cond == nil {
		cond = &metav1.Condition{
			Type:   conditionType,
			Status: metav1.ConditionUnknown,
		}
	}

	recheck := time.Since(cond.LastTransitionTime.Time) > time.Minute
	previouslySynced := cond.Status == metav1.ConditionTrue
	generationChanged := cond.ObservedGeneration != 0 && cond.ObservedGeneration < rp.Generation

	// NB: This controller re-queues fairly frequently as is (Watching STS
	// which watches Pods), so we're largely relying on that to ensure we eventually run our rechecks.
	return previouslySynced && !(generationChanged || recheck)
}

func (r *RedpandaReconciler) reconcileLicense(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if r.ratelimitCondition(ctx, rp, redpandav1alpha2.ClusterLicenseValid) {
		return nil
	}

	client, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return err
	}
	defer client.Close()

	features, err := client.GetEnterpriseFeatures(ctx)
	if err != nil {
		if internalclient.IsTerminalClientError(err) {
			apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
				Type:               redpandav1alpha2.ClusterLicenseValid,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: rp.Generation,
				Reason:             "TerminalError",
				Message:            err.Error(),
			})

			return nil
		}
		return err
	}

	licenseInfo, err := client.GetLicenseInfo(ctx)
	if err != nil {
		if internalclient.IsTerminalClientError(err) {
			apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
				Type:               redpandav1alpha2.ClusterLicenseValid,
				Status:             metav1.ConditionUnknown,
				ObservedGeneration: rp.Generation,
				Reason:             "TerminalError",
				Message:            err.Error(),
			})

			return nil
		}
		return err
	}

	var message string
	var reason string
	status := metav1.ConditionUnknown

	switch features.LicenseStatus {
	case rpadmin.LicenseStatusExpired:
		status = metav1.ConditionFalse
		reason = "LicenseExpired"
		message = "Expired"
	case rpadmin.LicenseStatusNotPresent:
		status = metav1.ConditionFalse
		reason = "LicenseNotPresent"
		message = "Not Present"
	case rpadmin.LicenseStatusValid:
		status = metav1.ConditionTrue
		reason = "LicenseValid"
		message = "Valid"
	}

	apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
		Type:               redpandav1alpha2.ClusterLicenseValid,
		Status:             status,
		ObservedGeneration: rp.Generation,
		Reason:             reason,
		Message:            message,
	})

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

	rp.Status.LicenseStatus = licenseStatus()

	return nil
}

func (r *RedpandaReconciler) reconcileClusterConfig(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if r.ratelimitCondition(ctx, rp, redpandav1alpha2.ClusterConfigSynced) {
		return nil
	}

	client, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return err
	}
	defer client.Close()

	config, err := r.clusterConfigFor(ctx, rp)
	if err != nil {
		return err
	}

	usersTXT, err := r.usersTXTFor(ctx, rp)
	if err != nil {
		return err
	}

	syncer := syncclusterconfig.Syncer{Client: client, Mode: syncclusterconfig.SyncerModeAdditive}

	if err := syncer.Sync(ctx, config, usersTXT); err != nil {
		return err
	}

	apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
		Type:               redpandav1alpha2.ClusterConfigSynced,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: rp.Generation,
		Reason:             "ConfigSynced",
	})

	return nil
}

func (r *RedpandaReconciler) usersTXTFor(ctx context.Context, rp *redpandav1alpha2.Redpanda) (map[string][]byte, error) {
	values, err := rp.GetValues()
	if err != nil {
		return nil, err
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
		return nil, err
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

	dot, err := rp.GetDot(&rest.Config{})
	if err != nil {
		return nil, err
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
		return nil, err
	}

	var desired map[string]any
	if err := yaml.Unmarshal([]byte(expanded), &desired); err != nil {
		return nil, err
	}

	return desired, nil
}

func (r *RedpandaReconciler) reconcileGC(ctx context.Context, rp *redpandav1alpha2.Redpanda, created map[gvkKey]struct{}) error {
	log := ctrl.LoggerFrom(ctx)

	types, err := allListTypes(r.Client)
	if err != nil {
		return err
	}

	// For all types in the redpanda helm chart,
	var toDelete []kube.Object
	for _, typ := range types {
		// Find all objects that have flux's internal label selector.
		if err := r.Client.List(ctx, typ, client.InNamespace(rp.Namespace), client.MatchingLabels{
			"helm.toolkit.fluxcd.io/name":      rp.GetHelmReleaseName(),
			"helm.toolkit.fluxcd.io/namespace": rp.Namespace,
		}); err != nil {
			// Some types from 3rd parties (monitoring, cert-manager) may not
			// exists. If they don't skip over them without erroring out.
			if apimeta.IsNoMatchError(err) {
				log.Info("Skipping unknown GVK", "gvk", fmt.Sprintf("%T", typ))
				continue
			}
			return err
		}

		if err := apimeta.EachListItem(typ, func(o runtime.Object) error {
			obj := o.(client.Object)

			gvk, err := r.Client.GroupVersionKindFor(obj)
			if err != nil {
				return errors.WithStack(err)
			}

			key := gvkKey{Key: client.ObjectKeyFromObject(obj), GVK: gvk}

			isOwned := slices.ContainsFunc(obj.GetOwnerReferences(), func(owner metav1.OwnerReference) bool {
				return owner.UID == rp.UID
			})

			// If we've just created this object, don't consider it for
			// deletion.
			if _, ok := created[key]; ok {
				return nil
			}

			// Similarly, if the object isn't owned by `rp`, don't consider it
			// for deletion.
			if !isOwned {
				return nil
			}

			toDelete = append(toDelete, obj)

			return nil
		}); err != nil {
			return err
		}
	}

	log.V(logger.TraceLevel).Info(fmt.Sprintf("identified %d objects to gc", len(toDelete)))

	var errs []error
	for _, obj := range toDelete {
		if err := r.Client.Delete(ctx, obj); err != nil {
			errs = append(errs, errors.Wrapf(err, "gc'ing %T: %s", obj, obj.GetName()))
		}
	}

	return errors.Join(errs...)
}

func (r *RedpandaReconciler) needsDecommission(ctx context.Context, rp *redpandav1alpha2.Redpanda, stses []*appsv1.StatefulSet) (bool, error) {
	client, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return false, err
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

func (r *RedpandaReconciler) reconcileDelete(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if controllerutil.ContainsFinalizer(rp, FinalizerKey) {
		controllerutil.RemoveFinalizer(rp, FinalizerKey)
		if err := r.Client.Update(ctx, rp); err != nil {
			return err
		}
	}
	return nil
}

func (r *RedpandaReconciler) patchRedpandaStatus(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	key := client.ObjectKeyFromObject(rp)
	latest := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return err
	}
	// HACK: Disable optimistic locking. Technically, the correct way to do
	// this is to set both objects' ResourceVersion to "". It's a waste of
	// resources deep copy rp, so we just copy it over.
	latest.ResourceVersion = rp.ResourceVersion
	return r.Client.Status().Patch(ctx, rp, client.MergeFrom(latest))
}

func (r *RedpandaReconciler) apply(ctx context.Context, obj client.Object) error {
	gvk, err := r.Client.GroupVersionKindFor(obj)
	if err != nil {
		return err
	}

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	return errors.WithStack(r.Client.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("redpanda-operator")))
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

func checkDeploymentsStatus(deployments []*appsv1.Deployment) (string, bool) {
	return checkReplicasForList(func(o *appsv1.Deployment) (int32, int32, int32, int32) {
		return o.Status.UpdatedReplicas, o.Status.AvailableReplicas, o.Status.ReadyReplicas, ptr.Deref(o.Spec.Replicas, 0)
	}, deployments, "Deployment")
}

func checkStatefulSetStatus(ss []*appsv1.StatefulSet) (string, bool) {
	return checkReplicasForList(func(o *appsv1.StatefulSet) (int32, int32, int32, int32) {
		return o.Status.UpdatedReplicas, o.Status.AvailableReplicas, o.Status.ReadyReplicas, ptr.Deref(o.Spec.Replicas, 0)
	}, ss, "StatefulSet")
}

type replicasExtractor[T client.Object] func(o T) (updated, available, ready, total int32)

func checkReplicasForList[T client.Object](fn replicasExtractor[T], list []T, resource string) (string, bool) {
	var notReady sort.StringSlice
	for _, item := range list {
		updated, available, ready, total := fn(item)

		if updated != total || available != total || ready != total {
			name := client.ObjectKeyFromObject(item).String()
			item := fmt.Sprintf("%q (updated/available/ready/total: %d/%d/%d/%d)", name, updated, available, ready, total)
			notReady = append(notReady, item)
		}
	}
	if len(notReady) > 0 {
		notReady.Sort()

		return fmt.Sprintf("Not all %s replicas updated, available, and ready for [%s]", resource, strings.Join(notReady, "; ")), false
	}
	return "", true
}

func allListTypes(c client.Client) ([]client.ObjectList, error) {
	// TODO: iterators would be really cool here.
	var types []client.ObjectList
	for _, t := range redpanda.Types() {
		gvk, err := c.GroupVersionKindFor(t)
		if err != nil {
			return nil, err
		}

		gvk.Kind += "List"

		list, err := c.Scheme().New(gvk)
		if err != nil {
			return nil, err
		}

		types = append(types, list.(client.ObjectList))
	}
	return types, nil
}
