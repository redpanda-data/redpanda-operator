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
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	helmv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/logger"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
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

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

const (
	FinalizerKey                    = "operator.redpanda.com/finalizer"
	ClusterConfigVersionKey         = "operator.redpanda.com/cluster-config-version"
	RestartClusterOnConfigChangeKey = "operator.redpanda.com/restart-cluster-on-config-change"
	FluxFinalizerKey                = "finalizers.fluxcd.io"

	NotManaged = "false"

	resourceReadyStrFmt    = "%s '%s/%s' is ready"
	resourceNotReadyStrFmt = "%s '%s/%s' is not ready"

	resourceTypeHelmRepository = "HelmRepository"
	resourceTypeHelmRelease    = "HelmRelease"

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
	KubeConfig         kube.Config
	Client             client.Client
	Scheme             *runtime.Scheme
	EventRecorder      kuberecorder.EventRecorder
	ClientFactory      internalclient.ClientFactory
	DefaultDisableFlux bool
	// HelmRepositorySpec.URL points to Redpanda helm repository where the following charts can be located:
	// * Redpanda
	// * Console
	// * Connectors
	// If not provided the v1alpha2.RedpandaChartRepository constant will be used.
	HelmRepositoryURL string
}

// flux resources main resources
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=buckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=buckets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=buckets/finalizers,verbs=update

// any resource that Redpanda helm creates and flux controller needs to reconcile them
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
		Owns(&sourcev1.HelmRepository{}).
		Owns(&helmv2beta1.HelmRelease{}).
		Owns(&helmv2beta2.HelmRelease{}).
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
				return ctrl.Result{}, errors.WithStack(err)
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
			return ctrl.Result{}, errors.WithStack(err)
		}
	}

	rp, err := r.reconcileFlux(ctx, rp)
	if err != nil {
		return ctrl.Result{}, errors.WithStack(err)
	}

	if err := r.reconcileDefluxed(ctx, rp); err != nil {
		return ctrl.Result{}, errors.WithStack(err)
	}

	if err := r.reconcileStatus(ctx, rp); err != nil {
		return ctrl.Result{}, errors.WithStack(err)
	}

	if err := r.reconcileLicense(ctx, rp); err != nil {
		return ctrl.Result{}, errors.WithStack(err)
	}

	if err := r.reconcileClusterConfig(ctx, rp); err != nil {
		return ctrl.Result{}, errors.WithStack(err)
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
	if !ptr.Deref(rp.Status.HelmRepositoryReady, false) {
		// strip out all of the requeues since this will get requeued based on the Owns in the setup of the reconciler
		msgNotReady := fmt.Sprintf(resourceNotReadyStrFmt, resourceTypeHelmRepository, rp.Namespace, rp.Status.HelmRepository)
		_ = redpandav1alpha2.RedpandaNotReady(rp, "ArtifactFailed", msgNotReady)
		return nil
	}

	if !ptr.Deref(rp.Status.HelmReleaseReady, false) {
		// strip out all of the requeues since this will get requeued based on the Owns in the setup of the reconciler
		msgNotReady := fmt.Sprintf(resourceNotReadyStrFmt, resourceTypeHelmRelease, rp.GetNamespace(), rp.GetHelmReleaseName())
		_ = redpandav1alpha2.RedpandaNotReady(rp, "ArtifactFailed", msgNotReady)
		return nil
	}

	// pull our deployments and stateful sets
	redpandaStatefulSets, err := redpandaStatefulSetsForCluster(ctx, r.Client, rp)
	if err != nil {
		return errors.WithStack(err)
	}

	consoleDeployments, err := consoleDeploymentsForCluster(ctx, r.Client, rp)
	if err != nil {
		return errors.WithStack(err)
	}

	connectorsDeployments, err := connectorsDeploymentsForCluster(ctx, r.Client, rp)
	if err != nil {
		return errors.WithStack(err)
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
		return errors.WithStack(err)
	}

	if needsDecommission {
		_ = redpandav1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", "Cluster currently decommissioning dead nodes")
		return nil
	}

	_ = redpandav1alpha2.RedpandaReady(rp)
	return nil
}

func (r *RedpandaReconciler) reconcileDefluxed(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx)

	if r.IsFluxEnabled(rp.Spec.ChartRef.UseFlux) {
		log.V(logger.TraceLevel).Info("useFlux is true; skipping non-flux reconciliation...")
		return nil
	}

	chartVersion := rp.Spec.ChartRef.ChartVersion
	desiredChartVersion := redpanda.Chart.Metadata().Version

	if !(chartVersion == "" || chartVersion == desiredChartVersion) {
		msg := fmt.Sprintf(".spec.chartRef.chartVersion needs to be %q or %q. got %q", desiredChartVersion, "", chartVersion)

		// NB: passing `nil` as err is acceptable for log.Error.
		log.Error(nil, msg, "chart version", rp.Spec.ChartRef.ChartVersion)
		r.EventRecorder.Eventf(rp, "Warning", redpandav1alpha2.EventSeverityError, msg)

		redpandav1alpha2.RedpandaNotReady(rp, "ChartRefUnsupported", msg)

		// Do not error out to not requeue. User needs to first migrate helm release to either "" or the pinned chart's version.
		return nil
	}

	// DeepCopy values to prevent any accidental mutations that may occur
	// within the chart itself.
	values := rp.Spec.ClusterSpec.DeepCopy()
	// The pods are being annotated with the cluster config version so that they
	// are restarted on any change to the cluster config.
	if rp.Annotations != nil && rp.Annotations[RestartClusterOnConfigChangeKey] == "true" {
		if c := apimeta.FindStatusCondition(rp.Status.Conditions, redpandav1alpha2.ClusterConfigSynced); c != nil {
			if values.Statefulset == nil {
				values.Statefulset = &redpandav1alpha2.Statefulset{}
			}
			if values.Statefulset.PodTemplate == nil {
				values.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{}
			}
			if values.Statefulset.PodTemplate.Annotations == nil {
				values.Statefulset.PodTemplate.Annotations = map[string]string{}
			}
			values.Statefulset.PodTemplate.Annotations[ClusterConfigVersionKey] = c.Message
		}
	}

	objs, err := redpanda.Chart.Render(&r.KubeConfig, helmette.Release{
		Namespace: rp.Namespace,
		Name:      rp.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, values)
	if err != nil {
		return errors.WithStack(err)
	}

	var errs []error

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
			errs = append(errs, errors.Wrapf(err, "deploying %T: %q", obj, obj.GetName()))
			continue
		}

		log.V(logger.TraceLevel).Info(fmt.Sprintf("deployed %T: %q", obj, obj.GetName()))

		// Record creation
		created[gvkKey{
			Key: client.ObjectKeyFromObject(obj),
			GVK: obj.GetObjectKind().GroupVersionKind(),
		}] = struct{}{}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
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
	if err := r.reconcileDefluxGC(ctx, rp, created); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (r *RedpandaReconciler) ratelimitCondition(ctx context.Context, rp *redpandav1alpha2.Redpanda, conditionType string) bool {
	log := ctrl.LoggerFrom(ctx)

	redpandaReady := apimeta.IsStatusConditionTrue(rp.Status.Conditions, meta.ReadyCondition)

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
		return errors.WithStack(err)
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
		return errors.WithStack(err)
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
		return errors.WithStack(err)
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

	if r.IsFluxEnabled(rp.Spec.ChartRef.UseFlux) {
		apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
			Type:               redpandav1alpha2.ClusterConfigSynced,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: rp.Generation,
			Reason:             "HandledByFlux",
			Message:            "cluster configuration is not managed by the operator when Flux is enabled",
		})
		return nil
	}

	client, err := r.ClientFactory.RedpandaAdminClient(ctx, rp)
	if err != nil {
		return errors.WithStack(err)
	}
	defer client.Close()

	config, err := r.clusterConfigFor(ctx, rp)
	if err != nil {
		return errors.WithStack(err)
	}

	usersTXT, err := r.usersTXTFor(ctx, rp)
	if err != nil {
		return errors.WithStack(err)
	}

	syncer := syncclusterconfig.Syncer{Client: client, Mode: syncclusterconfig.SyncerModeAdditive}
	configStatus, err := syncer.Sync(ctx, config, usersTXT)
	if err != nil {
		return errors.WithStack(err)
	}
	// As some configs won't be respected until all brokers have been restarted,
	// ClusterConfigSynced won't be reported as true until the config has been
	// synchronized AND no broker restarts are required.
	// `Reason` will indicate the difference between needing to sync a condition
	// and waiting for a Broker restarts.
	condition := metav1.ConditionTrue
	reason := "ConfigSynced"
	if configStatus.NeedsRestart {
		condition = metav1.ConditionFalse
		reason = "RestartRequired"
	}
	apimeta.SetStatusCondition(rp.GetConditions(), metav1.Condition{
		Type:               redpandav1alpha2.ClusterConfigSynced,
		Status:             condition,
		ObservedGeneration: rp.Generation,
		Reason:             reason,
		Message:            fmt.Sprintf("ClusterConfig at Version %d", configStatus.Version),
	})

	return nil
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

	dot, err := rp.GetDot(&rest.Config{})
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

func (r *RedpandaReconciler) reconcileDefluxGC(ctx context.Context, rp *redpandav1alpha2.Redpanda, created map[gvkKey]struct{}) error {
	log := ctrl.LoggerFrom(ctx)

	types, err := allListTypes(r.Client)
	if err != nil {
		return errors.WithStack(err)
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
			return errors.WithStack(err)
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
			return errors.WithStack(err)
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

func (r *RedpandaReconciler) reconcileFlux(ctx context.Context, rp *redpandav1alpha2.Redpanda) (*redpandav1alpha2.Redpanda, error) {
	log := ctrl.LoggerFrom(ctx)
	log.WithName("RedpandaReconciler.reconcile")

	// Check if HelmRepository exists or create it
	if err := r.reconcileHelmRepository(ctx, rp); err != nil {
		return rp, errors.WithStack(err)
	}

	if !ptr.Deref(rp.Status.HelmRepositoryReady, false) {
		return rp, nil
	}

	// Check if HelmRelease exists or create it also
	if err := r.reconcileHelmRelease(ctx, rp); err != nil {
		return rp, errors.WithStack(err)
	}

	return rp, nil
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

func (r *RedpandaReconciler) reconcileHelmRelease(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	hr, err := r.createHelmReleaseFromTemplate(ctx, rp)
	if err != nil {
		return errors.WithStack(err)
	}

	if err := r.apply(ctx, hr); err != nil {
		return errors.WithStack(err)
	}

	isGenerationCurrent := hr.Generation == hr.Status.ObservedGeneration
	isStatusConditionReady := apimeta.IsStatusConditionTrue(hr.Status.Conditions, meta.ReadyCondition) || apimeta.IsStatusConditionTrue(hr.Status.Conditions, helmv2beta2.RemediatedCondition)

	// When UseFlux is false, we suspend the HelmRelease which completely
	// disables the controller. In such cases, we have to lie a bit to keep
	// everything else chugging along as expected.
	if hr.Spec.Suspend {
		isGenerationCurrent = true
		isStatusConditionReady = true
	}

	rp.Status.HelmRelease = hr.Name
	rp.Status.HelmReleaseReady = ptr.To(isGenerationCurrent && isStatusConditionReady)

	return nil
}

func (r *RedpandaReconciler) reconcileHelmRepository(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	repo := r.HelmRepositoryFromTemplate(rp)

	if err := r.apply(ctx, repo); err != nil {
		return fmt.Errorf("applying HelmRepository: %w", err)
	}

	isGenerationCurrent := repo.Generation == repo.Status.ObservedGeneration
	isStatusConditionReady := apimeta.IsStatusConditionTrue(repo.Status.Conditions, meta.ReadyCondition)

	// When UseFlux is false, we suspend the HelmRepository which completely
	// disables the controller. In such cases, we have to lie a bit to keep
	// everything else chugging along as expected.
	if repo.Spec.Suspend {
		isGenerationCurrent = true
		isStatusConditionReady = true
	}

	rp.Status.HelmRepository = repo.Name
	rp.Status.HelmRepositoryReady = ptr.To(isStatusConditionReady && isGenerationCurrent)

	return nil
}

func (r *RedpandaReconciler) reconcileDelete(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	if err := r.deleteHelmChart(ctx, rp); err != nil {
		return errors.WithStack(err)
	}

	if controllerutil.ContainsFinalizer(rp, FinalizerKey) {
		controllerutil.RemoveFinalizer(rp, FinalizerKey)
		if err := r.Client.Update(ctx, rp); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func (r *RedpandaReconciler) deleteHelmChart(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	// When HelmRelease is suspended it will not delete child resource HelmChart. In this function there is attempt
	// to delete HelmChart custom resource.
	// Reference:
	// https://github.com/fluxcd/helm-controller/blob/2d335f2aa0e2e0df2a631ebf19394aed07c556f3/internal/reconcile/helmchart_template.go#L163-L166
	// https://github.com/fluxcd/helm-controller/blob/2d335f2aa0e2e0df2a631ebf19394aed07c556f3/internal/controller/helmrelease_controller.go#L375-L388
	namespacedName := client.ObjectKey{Namespace: rp.Namespace, Name: rp.Namespace + "-" + rp.Name}
	var chart sourcev1.HelmChart
	err := r.Client.Get(ctx, namespacedName, &chart)
	if err != nil && !apierrors.IsNotFound(err) {
		// Return error to retry until we succeed.
		return fmt.Errorf("failed to get flux HelmChart '%s': %w", namespacedName.Name, err)
	} else if err == nil {
		if controllerutil.ContainsFinalizer(&chart, FluxFinalizerKey) {
			controllerutil.RemoveFinalizer(&chart, FluxFinalizerKey)
			if err := r.Client.Update(ctx, &chart); err != nil {
				return fmt.Errorf("failed to update flux HelmChart '%s': %w", namespacedName.Name, err)
			}
		}

		// Delete the HelmChart.
		if err = r.Client.Delete(ctx, &chart); err != nil {
			return fmt.Errorf("failed to delete flux HelmChart '%s': %w", namespacedName.Name, err)
		}
	}

	return nil
}

func (r *RedpandaReconciler) createHelmReleaseFromTemplate(ctx context.Context, rp *redpandav1alpha2.Redpanda) (*helmv2beta2.HelmRelease, error) {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.createHelmReleaseFromTemplate")

	values, err := rp.ValuesJSON()
	if err != nil {
		return nil, fmt.Errorf("could not parse clusterSpec to json: %w", err)
	}

	log.V(logger.DebugLevel).Info("helm release values", "raw-values", string(values.Raw))

	hasher := sha256.New()
	hasher.Write(values.Raw)
	sha := base64.URLEncoding.EncodeToString(hasher.Sum(nil))
	// TODO possibly add the SHA to the status
	log.V(logger.TraceLevel).Info(fmt.Sprintf("SHA of values file to use: %s", sha))

	timeout := rp.Spec.ChartRef.Timeout
	if timeout == nil {
		timeout = &metav1.Duration{Duration: 15 * time.Minute}
	}

	chartVersion := rp.Spec.ChartRef.ChartVersion
	if chartVersion == "" {
		chartVersion = redpanda.Chart.Metadata().Version
	}

	upgrade := &helmv2beta2.Upgrade{
		// we skip waiting since relying on the Helm release process
		// to actually happen means that we block running any sort
		// of pending upgrades while we are attempting the upgrade job.
		DisableWait:        true,
		DisableWaitForJobs: true,
	}

	helmUpgrade := rp.Spec.ChartRef.Upgrade
	if rp.Spec.ChartRef.Upgrade != nil {
		if helmUpgrade.Force != nil {
			upgrade.Force = ptr.Deref(helmUpgrade.Force, false)
		}
		if helmUpgrade.CleanupOnFail != nil {
			upgrade.CleanupOnFail = ptr.Deref(helmUpgrade.CleanupOnFail, false)
		}
		if helmUpgrade.PreserveValues != nil {
			upgrade.PreserveValues = ptr.Deref(helmUpgrade.PreserveValues, false)
		}
		if helmUpgrade.Remediation != nil {
			upgrade.Remediation = helmUpgrade.Remediation
		}
	}

	return &helmv2beta2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rp.GetHelmReleaseName(),
			Namespace:       rp.Namespace,
			OwnerReferences: []metav1.OwnerReference{rp.OwnerShipRefObj()},
		},
		Spec: helmv2beta2.HelmReleaseSpec{
			Suspend: !r.IsFluxEnabled(rp.Spec.ChartRef.UseFlux),
			Chart: helmv2beta2.HelmChartTemplate{
				Spec: helmv2beta2.HelmChartTemplateSpec{
					Chart:    "redpanda",
					Version:  chartVersion,
					Interval: &metav1.Duration{Duration: 1 * time.Minute},
					SourceRef: helmv2beta2.CrossNamespaceObjectReference{
						Kind:      "HelmRepository",
						Name:      rp.GetHelmRepositoryName(),
						Namespace: rp.Namespace,
					},
				},
			},
			Values:   values,
			Interval: metav1.Duration{Duration: 30 * time.Second},
			Timeout:  timeout,
			Upgrade:  upgrade,
			Install: &helmv2beta2.Install{
				Remediation: &helmv2beta2.InstallRemediation{
					// Per the flux helm remediation docs negative value is set to
					// reconcile even if client-side cache has stale data.
					//
					// Flux Docs:
					// Retries is the number of retries that should be attempted on
					// failures before bailing. Remediation, using an uninstall,
					// is performed between each attempt. Defaults to '0', a negative
					// integer equals to unlimited retries.
					Retries: -1,
				},
			},
		},
	}, nil
}

func (r *RedpandaReconciler) HelmRepositoryFromTemplate(rp *redpandav1alpha2.Redpanda) *sourcev1.HelmRepository {
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rp.GetHelmRepositoryName(),
			Namespace:       rp.Namespace,
			OwnerReferences: []metav1.OwnerReference{rp.OwnerShipRefObj()},
		},
		Spec: sourcev1.HelmRepositorySpec{
			Suspend:  !r.IsFluxEnabled(rp.Spec.ChartRef.UseFlux),
			Interval: metav1.Duration{Duration: 30 * time.Second},
			URL:      r.HelmRepositoryURL,
		},
	}
}

func (r *RedpandaReconciler) patchRedpandaStatus(ctx context.Context, rp *redpandav1alpha2.Redpanda) error {
	key := client.ObjectKeyFromObject(rp)
	latest := &redpandav1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return errors.WithStack(err)
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
		return errors.WithStack(err)
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
			return nil, errors.WithStack(err)
		}

		gvk.Kind += "List"

		list, err := c.Scheme().New(gvk)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		types = append(types, list.(client.ObjectList))
	}
	return types, nil
}

func (r *RedpandaReconciler) IsFluxEnabled(useFlux *bool) bool {
	return ptr.Deref(useFlux, !r.DefaultDisableFlux)
}
