// Copyright 2024 Redpanda Data, Inc.
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
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	helmv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/logger"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	v2 "sigs.k8s.io/controller-runtime/pkg/webhook/conversion/testdata/api/v2"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

const (
	resourceReadyStrFmt    = "%s '%s/%s' is ready"
	resourceNotReadyStrFmt = "%s '%s/%s' is not ready"

	resourceTypeHelmRepository = "HelmRepository"
	resourceTypeHelmRelease    = "HelmRelease"

	managedPath = "/managed"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"

	HelmChartConstraint = "5.9.3"
)

var errWaitForReleaseDeletion = errors.New("wait for helm release deletion")

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	kuberecorder.EventRecorder
}

// flux resources main resources
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,namespace=default,resources=helmreleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmcharts/finalizers,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=helmrepositories/finalizers,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepository,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepository/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepository/finalizers,verbs=get;create;update;patch;delete

// flux additional resources
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=buckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,namespace=default,resources=gitrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=replicasets,verbs=get;list;watch;create;update;patch;delete

// any resource that Redpanda helm creates and flux controller needs to reconcile them
// +kubebuilder:rbac:groups="",namespace=default,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=default,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=default,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,namespace=default,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,namespace=default,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=policy,namespace=default,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=default,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,namespace=default,resources=certificates,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,namespace=default,resources=issuers,verbs=get;create;update;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",namespace=default,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,namespace=default,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// for the migration purposes to disable reconciliation of cluster and console custom resources
// +kubebuilder:rbac:groups=redpanda.vectorized.io,namespace=default,resources=clusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=redpanda.vectorized.io,namespace=default,resources=consoles,verbs=get;list;watch;update;patch

// redpanda resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=redpandas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=redpandas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,namespace=default,resources=redpandas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,namespace=default,resources=events,verbs=create;patch

// SetupWithManager sets up the controller with the Manager.
func (r *RedpandaReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := registerStatefulSetClusterIndex(ctx, mgr); err != nil {
		return err
	}
	if err := registerDeploymentClusterIndex(ctx, mgr); err != nil {
		return err
	}

	helmManagedComponentPredicate, err := predicate.LabelSelectorPredicate(
		metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      "app.kubernetes.io/name", // look for only redpanda or console pods
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"redpanda", "console"},
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

	enqueueRequestFromManaged := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
		if nn, found := clusterForHelmManagedObject(o); found {
			return []reconcile.Request{{NamespacedName: nn}}
		}
		return nil
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha2.Redpanda{}).
		Owns(&sourcev1.HelmRepository{}).
		Owns(&helmv2beta1.HelmRelease{}).
		Owns(&helmv2beta2.HelmRelease{}).
		Watches(&appsv1.StatefulSet{}, enqueueRequestFromManaged, managedWatchOption).
		Watches(&appsv1.Deployment{}, enqueueRequestFromManaged, managedWatchOption).
		Complete(r)
}

func (r *RedpandaReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.Reconcile")

	defer func() {
		durationMsg := fmt.Sprintf("reconciliation finished in %s", time.Since(start).String())
		log.Info(durationMsg)
	}()

	log.Info("Starting reconcile loop")

	rp := &v1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, req.NamespacedName, rp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Examine if the object is under deletion
	if !rp.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rp)
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
		log.Info("Managed decommission")
		return ctrl.Result{}, nil
	}

	// add finalizer if not exist
	if !controllerutil.ContainsFinalizer(rp, FinalizerKey) {
		patch := client.MergeFrom(rp.DeepCopy())
		controllerutil.AddFinalizer(rp, FinalizerKey)
		if err := r.Patch(ctx, rp, patch); err != nil {
			log.Error(err, "unable to register finalizer")
			return ctrl.Result{}, err
		}
	}

	rp, err := r.reconcile(ctx, rp)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.reconcileDefluxed(ctx, rp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update status after reconciliation.
	if updateStatusErr := r.patchRedpandaStatus(ctx, rp); updateStatusErr != nil {
		log.Error(updateStatusErr, "unable to update status after reconciliation")
		return ctrl.Result{}, updateStatusErr
	}

	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileDefluxed(ctx context.Context, rp *v1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx)
	log.WithName("RedpandaReconciler.reconcileDefluxed")

	if !ptr.Deref(rp.Spec.ChartRef.UseFlux, true) {
		// TODO (Rafal) Implement Redpanda helm chart templating with Redpanda Status Report
		// In the Redpanda.Status there will be only Conditions and Failures that would be used.

		if !atLeast(rp.Spec.ChartRef.ChartVersion) {
			log.Error(fmt.Errorf("chart version needs to be at least %s", HelmChartConstraint), "", "chart version", rp.Spec.ChartRef.ChartVersion)
			v1alpha2.RedpandaNotReady(rp, "ChartRefUnsupported", fmt.Sprintf("chart version needs to be at least %s. Currently it is %s", HelmChartConstraint, rp.Spec.ChartRef.ChartVersion))
			r.EventRecorder.Eventf(rp, "Warning", v1alpha2.EventSeverityError, fmt.Sprintf("chart version needs to be at least %s. Currently it is %s", HelmChartConstraint, rp.Spec.ChartRef.ChartVersion))
			// Do not error out to not requeue. User needs to first migrate helm release to at least 5.9.3 version
			return nil
		}
	}
	return nil
}

func atLeast(version string) bool {
	if version == "" {
		return true
	}

	c, err := semver.NewConstraint(fmt.Sprintf(">= %s", HelmChartConstraint))
	if err != nil {
		// Handle constraint not being parsable.
		return false
	}

	v, err := semver.NewVersion(version)
	if err != nil {
		return false
	}

	return c.Check(v)
}

func (r *RedpandaReconciler) reconcile(ctx context.Context, rp *v1alpha2.Redpanda) (*v1alpha2.Redpanda, error) {
	log := ctrl.LoggerFrom(ctx)
	log.WithName("RedpandaReconciler.reconcile")

	// Observe HelmRelease generation.
	if rp.Status.ObservedGeneration != rp.Generation {
		rp.Status.ObservedGeneration = rp.Generation
		rp = v1alpha2.RedpandaProgressing(rp)
		if updateStatusErr := r.patchRedpandaStatus(ctx, rp); updateStatusErr != nil {
			log.Error(updateStatusErr, "unable to update status after generation update")
			return rp, updateStatusErr
		}
	}

	// pull our deployments and stateful sets
	redpandaStatefulSets, err := redpandaStatefulSetsForCluster(ctx, r.Client, rp)
	if err != nil {
		return rp, err
	}
	consoleDeployments, err := consoleDeploymentsForCluster(ctx, r.Client, rp)
	if err != nil {
		return rp, err
	}

	// Check if HelmRepository exists or create it
	rp, repo, err := r.reconcileHelmRepository(ctx, rp)
	if err != nil {
		return rp, err
	}

	isGenerationCurrent := repo.Generation != repo.Status.ObservedGeneration
	isStatusConditionReady := apimeta.IsStatusConditionTrue(repo.Status.Conditions, meta.ReadyCondition)
	msgNotReady := fmt.Sprintf(resourceNotReadyStrFmt, resourceTypeHelmRepository, repo.GetNamespace(), repo.GetName())
	msgReady := fmt.Sprintf(resourceReadyStrFmt, resourceTypeHelmRepository, repo.GetNamespace(), repo.GetName())
	isStatusReadyNILorTRUE := ptr.Equal(rp.Status.HelmRepositoryReady, ptr.To(true))
	isStatusReadyNILorFALSE := ptr.Equal(rp.Status.HelmRepositoryReady, ptr.To(false))

	isResourceReady := r.checkIfResourceIsReady(log, msgNotReady, msgReady, resourceTypeHelmRepository, isGenerationCurrent, isStatusConditionReady, isStatusReadyNILorTRUE, isStatusReadyNILorFALSE, rp)
	if !isResourceReady {
		// strip out all of the requeues since this will get requeued based on the Owns in the setup of the reconciler
		return v1alpha2.RedpandaNotReady(rp, "ArtifactFailed", msgNotReady), nil
	}

	// Check if HelmRelease exists or create it also
	rp, hr, err := r.reconcileHelmRelease(ctx, rp)
	if err != nil {
		return rp, err
	}
	if hr.Name == "" {
		log.Info(fmt.Sprintf("Created HelmRelease for '%s/%s', will requeue", rp.Namespace, rp.Name))
		return rp, err
	}

	isGenerationCurrent = hr.Generation != hr.Status.ObservedGeneration
	isStatusConditionReady = apimeta.IsStatusConditionTrue(hr.Status.Conditions, meta.ReadyCondition) || apimeta.IsStatusConditionTrue(hr.Status.Conditions, helmv2beta2.RemediatedCondition)
	msgNotReady = fmt.Sprintf(resourceNotReadyStrFmt, resourceTypeHelmRelease, hr.GetNamespace(), hr.GetName())
	msgReady = fmt.Sprintf(resourceReadyStrFmt, resourceTypeHelmRelease, hr.GetNamespace(), hr.GetName())
	isStatusReadyNILorTRUE = ptr.Equal(rp.Status.HelmReleaseReady, ptr.To(true))
	isStatusReadyNILorFALSE = ptr.Equal(rp.Status.HelmReleaseReady, ptr.To(false))

	isResourceReady = r.checkIfResourceIsReady(log, msgNotReady, msgReady, resourceTypeHelmRelease, isGenerationCurrent, isStatusConditionReady, isStatusReadyNILorTRUE, isStatusReadyNILorFALSE, rp)
	if !isResourceReady {
		// strip out all of the requeues since this will get requeued based on the Owns in the setup of the reconciler
		return v1alpha2.RedpandaNotReady(rp, "ArtifactFailed", msgNotReady), nil
	}

	for _, sts := range redpandaStatefulSets {
		decommission, err := needsDecommission(ctx, sts, log)
		if err != nil {
			return rp, err
		}
		if decommission {
			return v1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", "Cluster currently decommissioning dead nodes"), nil
		}
	}

	// check to make sure that our stateful set pods are all current
	if message, ready := checkStatefulSetStatus(redpandaStatefulSets); !ready {
		return v1alpha2.RedpandaNotReady(rp, "RedpandaPodsNotReady", message), nil
	}

	// check to make sure that our deployment pods are all current
	if message, ready := checkDeploymentsStatus(consoleDeployments); !ready {
		return v1alpha2.RedpandaNotReady(rp, "ConsolePodsNotReady", message), nil
	}

	return v1alpha2.RedpandaReady(rp), nil
}

func (r *RedpandaReconciler) checkIfResourceIsReady(log logr.Logger, msgNotReady, msgReady, kind string, isGenerationCurrent, isStatusConditionReady, isStatusReadyNILorTRUE, isStatusReadyNILorFALSE bool, rp *v1alpha2.Redpanda) bool {
	if isGenerationCurrent || !isStatusConditionReady {
		// capture event only
		if isStatusReadyNILorTRUE {
			r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityInfo, msgNotReady)
		}

		switch kind {
		case resourceTypeHelmRepository:
			rp.Status.HelmRepositoryReady = ptr.To(false)
		case resourceTypeHelmRelease:
			rp.Status.HelmReleaseReady = ptr.To(false)
		}

		log.Info(msgNotReady)
		return false
	} else if isStatusConditionReady && isStatusReadyNILorFALSE {
		// here since the condition should be true, we update the value to
		// be true, and send an event
		switch kind {
		case resourceTypeHelmRepository:
			rp.Status.HelmRepositoryReady = ptr.To(true)
		case resourceTypeHelmRelease:
			rp.Status.HelmReleaseReady = ptr.To(true)
		}

		r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityInfo, msgReady)
	}

	return true
}

func (r *RedpandaReconciler) reconcileHelmRelease(ctx context.Context, rp *v1alpha2.Redpanda) (*v1alpha2.Redpanda, *helmv2beta2.HelmRelease, error) {
	var err error

	// Check if HelmRelease exists or create it
	hr := &helmv2beta2.HelmRelease{}

	// have we recorded a helmRelease, if not assume we have not created it
	if rp.Status.HelmRelease == "" {
		// did not find helmRelease, then create it
		hr, err = r.createHelmRelease(ctx, rp)
		return rp, hr, err
	}

	// if we are not empty, then we assume at some point this existed, let's check
	key := types.NamespacedName{Namespace: rp.Namespace, Name: rp.Status.GetHelmRelease()}
	err = r.Client.Get(ctx, key, hr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			rp.Status.HelmRelease = ""
			hr, err = r.createHelmRelease(ctx, rp)
			return rp, hr, err
		}
		// if this is a not found error
		return rp, hr, fmt.Errorf("failed to get HelmRelease '%s/%s': %w", rp.Namespace, rp.Status.HelmRelease, err)
	}

	// Check if we need to update here
	hrTemplate, errTemplated := r.createHelmReleaseFromTemplate(ctx, rp)
	if errTemplated != nil {
		r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, errTemplated.Error())
		return rp, hr, errTemplated
	}

	if r.helmReleaseRequiresUpdate(ctx, hr, hrTemplate) {
		hr.Spec = hrTemplate.Spec
		if err = r.Client.Update(ctx, hr); err != nil {
			r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, err.Error())
			return rp, hr, err
		}
		r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityInfo, fmt.Sprintf("HelmRelease '%s/%s' updated", rp.Namespace, rp.GetHelmReleaseName()))
		rp.Status.HelmRelease = rp.GetHelmReleaseName()
	}

	return rp, hr, nil
}

func (r *RedpandaReconciler) reconcileHelmRepository(ctx context.Context, rp *v1alpha2.Redpanda) (*v1alpha2.Redpanda, *sourcev1.HelmRepository, error) {
	// Check if HelmRepository exists or create it
	repo := &sourcev1.HelmRepository{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: rp.GetHelmRepositoryName()}, repo); err != nil {
		if apierrors.IsNotFound(err) {
			repo = r.createHelmRepositoryFromTemplate(rp)
			if errCreate := r.Client.Create(ctx, repo); errCreate != nil {
				r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, fmt.Sprintf("error creating HelmRepository: %s", errCreate))
				return rp, repo, fmt.Errorf("error creating HelmRepository: %w", errCreate)
			}
			r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityInfo, fmt.Sprintf("HelmRepository '%s/%s' created ", rp.Namespace, rp.GetHelmRepositoryName()))
		} else {
			r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, fmt.Sprintf("error getting HelmRepository: %s", err))
			return rp, repo, fmt.Errorf("error getting HelmRepository: %w", err)
		}
	}
	rp.Status.HelmRepository = rp.GetHelmRepositoryName()

	return rp, repo, nil
}

func (r *RedpandaReconciler) reconcileDelete(ctx context.Context, rp *v1alpha2.Redpanda) (ctrl.Result, error) {
	if err := r.deleteHelmRelease(ctx, rp); err != nil {
		return ctrl.Result{}, err
	}
	if controllerutil.ContainsFinalizer(rp, FinalizerKey) {
		controllerutil.RemoveFinalizer(rp, FinalizerKey)
		if err := r.Client.Update(ctx, rp); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) createHelmRelease(ctx context.Context, rp *v1alpha2.Redpanda) (*helmv2beta2.HelmRelease, error) {
	// create helmRelease resource from template
	hRelease, err := r.createHelmReleaseFromTemplate(ctx, rp)
	if err != nil {
		r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, fmt.Sprintf("could not create helm release template: %s", err))
		return hRelease, fmt.Errorf("could not create HelmRelease template: %w", err)
	}

	// create helmRelease object here
	if err := r.Client.Create(ctx, hRelease); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityError, err.Error())
			return hRelease, fmt.Errorf("failed to create HelmRelease '%s/%s': %w", rp.Namespace, rp.Status.HelmRelease, err)
		}
		// we already exist, then update the status to rp
		rp.Status.HelmRelease = rp.GetHelmReleaseName()
	}

	// we have created the resource, so we are ok to update events, and update the helmRelease name on the status object
	r.event(rp, rp.Status.LastAttemptedRevision, v1alpha2.EventSeverityInfo, fmt.Sprintf("HelmRelease '%s/%s' created ", rp.Namespace, rp.GetHelmReleaseName()))
	rp.Status.HelmRelease = rp.GetHelmReleaseName()

	return hRelease, nil
}

func (r *RedpandaReconciler) deleteHelmRelease(ctx context.Context, rp *v1alpha2.Redpanda) error {
	if rp.Status.HelmRelease == "" {
		return nil
	}

	var hr helmv2beta2.HelmRelease
	hrName := rp.Status.GetHelmRelease()
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: rp.Namespace, Name: hrName}, &hr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			rp.Status.HelmRelease = ""
			rp.Status.HelmRepository = ""
			return nil
		}
		return fmt.Errorf("failed to get HelmRelease '%s': %w", rp.Status.HelmRelease, err)
	}

	foregroundDeletePropagation := metav1.DeletePropagationForeground

	if err = r.Client.Delete(ctx, &hr, &client.DeleteOptions{
		PropagationPolicy: &foregroundDeletePropagation,
	}); err != nil {
		return fmt.Errorf("deleting helm release connected with Redpanda (%s): %w", rp.Name, err)
	}

	return errWaitForReleaseDeletion
}

func (r *RedpandaReconciler) createHelmReleaseFromTemplate(ctx context.Context, rp *v1alpha2.Redpanda) (*helmv2beta2.HelmRelease, error) {
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
	log.Info(fmt.Sprintf("SHA of values file to use: %s", sha))

	timeout := rp.Spec.ChartRef.Timeout
	if timeout == nil {
		timeout = &metav1.Duration{Duration: 15 * time.Minute}
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
			Suspend: !ptr.Deref(rp.Spec.ChartRef.UseFlux, true),
			Chart: helmv2beta2.HelmChartTemplate{
				Spec: helmv2beta2.HelmChartTemplateSpec{
					Chart:    "redpanda",
					Version:  rp.Spec.ChartRef.ChartVersion,
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
		},
	}, nil
}

func (r *RedpandaReconciler) createHelmRepositoryFromTemplate(rp *v1alpha2.Redpanda) *sourcev1.HelmRepository {
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rp.GetHelmRepositoryName(),
			Namespace:       rp.Namespace,
			OwnerReferences: []metav1.OwnerReference{rp.OwnerShipRefObj()},
		},
		Spec: sourcev1.HelmRepositorySpec{
			Suspend:  !ptr.Deref(rp.Spec.ChartRef.UseFlux, true),
			Interval: metav1.Duration{Duration: 30 * time.Second},
			URL:      v1alpha2.RedpandaChartRepository,
		},
	}
}

func (r *RedpandaReconciler) patchRedpandaStatus(ctx context.Context, rp *v1alpha2.Redpanda) error {
	key := client.ObjectKeyFromObject(rp)
	latest := &v1alpha2.Redpanda{}
	if err := r.Client.Get(ctx, key, latest); err != nil {
		return err
	}
	return r.Client.Status().Patch(ctx, rp, client.MergeFrom(latest))
}

// event emits a Kubernetes event and forwards the event to notification controller if configured.
func (r *RedpandaReconciler) event(rp *v1alpha2.Redpanda, revision, severity, msg string) {
	var metaData map[string]string
	if revision != "" {
		metaData = map[string]string{v2.GroupVersion.Group + revisionPath: revision}
	}
	eventType := "Normal"
	if severity == v1alpha2.EventSeverityError {
		eventType = "Warning"
	}
	r.EventRecorder.AnnotatedEventf(rp, metaData, eventType, severity, msg)
}

func (r *RedpandaReconciler) helmReleaseRequiresUpdate(ctx context.Context, hr, hrTemplate *helmv2beta2.HelmRelease) bool {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.helmReleaseRequiresUpdate")

	switch {
	case !reflect.DeepEqual(hr.GetValues(), hrTemplate.GetValues()):
		log.Info("values found different")
		return true
	case helmChartRequiresUpdate(log, &hr.Spec.Chart, &hrTemplate.Spec.Chart):
		log.Info("chartTemplate found different")
		return true
	case hr.Spec.Interval != hrTemplate.Spec.Interval:
		log.Info("interval found different")
		return true
	default:
		return false
	}
}

// helmChartRequiresUpdate compares the v2beta1.HelmChartTemplate of the
// v2beta1.HelmRelease to the given v1beta2.HelmChart to determine if an
// update is required.
func helmChartRequiresUpdate(log logr.Logger, template, chart *helmv2beta2.HelmChartTemplate) bool {
	switch {
	case template.Spec.Chart != chart.Spec.Chart:
		log.Info("chart is different")
		return true
	case template.Spec.Version != "" && template.Spec.Version != chart.Spec.Version:
		log.Info("spec version is different")
		return true
	default:
		return false
	}
}

func isRedpandaManaged(ctx context.Context, redpandaCluster *v1alpha2.Redpanda) bool {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.isRedpandaManaged")

	managedAnnotationKey := v1alpha2.GroupVersion.Group + managedPath
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
