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
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	"github.com/redpanda-data/helm-charts/pkg/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
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

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	vectorzied_v1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/vectorized/v1alpha1"
	consolepkg "github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/console"
	"github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/resources"
)

const (
	managedPath = "/managed"

	revisionPath        = "/revision"
	componentLabelValue = "redpanda-statefulset"
)

var errWaitForReleaseDeletion = errors.New("wait for helm release deletion")

// RedpandaReconciler reconciles a Redpanda object
type RedpandaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	kuberecorder.EventRecorder

	kubeConfig kube.Config
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
		Watches(&appsv1.StatefulSet{}, enqueueRequestFromManaged, managedWatchOption).
		Watches(&appsv1.Deployment{}, enqueueRequestFromManaged, managedWatchOption).
		Complete(r)
}

func (r *RedpandaReconciler) Reconcile(c context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, done := context.WithCancel(c)
	defer done()

	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.Reconcile")

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

	// TODO There needs to be tests to validate migration from operator v1 to operator v2 (Cluster to Redpanda and maybe vice versa)
	//if rp.Spec.Migration != nil && rp.Spec.Migration.Enabled {
	//	if err := r.tryMigration(ctx, log, rp); err != nil {
	//		log.Error(err, "migration")
	//	}
	//}

	rp, result, err := r.reconcile(ctx, rp)

	// Update status after reconciliation.
	if updateStatusErr := r.reconcileStatus(ctx, rp); updateStatusErr != nil {
		log.Error(updateStatusErr, "unable to update status after reconciliation")
		return ctrl.Result{}, updateStatusErr
	}

	// Log reconciliation duration
	// TODO (Rafal) Check if there is metrics for that. Maybe remove it?
	durationMsg := fmt.Sprintf("reconciliation finished in %s", time.Since(start).String())
	log.Info(durationMsg)

	return result, err
}

type resourceToMigrate struct {
	resourceName string
	helperString string
	resource     client.Object
}

func (r *RedpandaReconciler) tryMigration(ctx context.Context, log logr.Logger, rp *v1alpha2.Redpanda) error {
	log = log.WithName("tryMigration")
	var errorResult error

	var cluster vectorzied_v1alpha1.Cluster
	clusterNamespace := rp.GetMigrationClusterNamespace()
	clusterName := rp.GetMigrationClusterName()
	err := r.Get(ctx, types.NamespacedName{
		Namespace: clusterNamespace,
		Name:      clusterName,
	}, &cluster)
	if err != nil {
		errorResult = errors.Join(fmt.Errorf("get cluster reference (%s/%s): %w", clusterNamespace, clusterName, err), errorResult)
	} else if isRedpandaClusterManaged(log, &cluster) {
		annotatedCluster := cluster.DeepCopy()
		disableRedpandaReconciliation(annotatedCluster)

		err = r.Update(ctx, annotatedCluster)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("disabling Cluster reconciliation (%s): %w", annotatedCluster.Name, err), errorResult)
		}

		msg := "update Cluster custom resource"
		log.V(DebugLevel).Info(msg, "cluster-name", annotatedCluster.Name, "annotations", annotatedCluster.Annotations, "finalizers", annotatedCluster.Finalizers)
		r.EventRecorder.AnnotatedEventf(annotatedCluster, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}

	var console vectorzied_v1alpha1.Console
	consoleNamespace := rp.GetMigrationConsoleNamespace()
	consoleName := rp.GetMigrationConsoleName()
	err = r.Get(ctx, types.NamespacedName{
		Namespace: consoleNamespace,
		Name:      consoleName,
	}, &console)
	if err != nil {
		errorResult = errors.Join(fmt.Errorf("get cluster reference (%s/%s): %w", consoleNamespace, consoleName, err), errorResult)
	} else if isConsoleManaged(log, &console) ||
		controllerutil.ContainsFinalizer(&console, consolepkg.ConsoleSAFinalizer) ||
		controllerutil.ContainsFinalizer(&console, consolepkg.ConsoleACLFinalizer) {
		annotatedConsole := console.DeepCopy()
		disableConsoleReconciliation(annotatedConsole)
		controllerutil.RemoveFinalizer(annotatedConsole, consolepkg.ConsoleSAFinalizer)
		controllerutil.RemoveFinalizer(annotatedConsole, consolepkg.ConsoleACLFinalizer)

		err = r.Update(ctx, annotatedConsole)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("disabling Cluster reconciliation (%s): %w", annotatedConsole.Name, err), errorResult)
		}

		msg := "update Console custom resource"
		log.V(DebugLevel).Info(msg, "console-name", annotatedConsole.Name, "annotations", annotatedConsole.Annotations, "finalizers", annotatedConsole.Finalizers)
		r.EventRecorder.AnnotatedEventf(annotatedConsole, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}

	redpandaResourcesToMigrate, err := r.tryMigrateRedpanda(ctx, log, rp)
	if err != nil {
		errorResult = errors.Join(err, errorResult)
	}

	var allResourcesToMigrate []resourceToMigrate
	allResourcesToMigrate = append(allResourcesToMigrate, redpandaResourcesToMigrate...)

	if ptr.Deref(rp.Spec.ClusterSpec.Console.Enabled, true) {
		consoleResourcesToMigrate, err := r.tryMigrateConsole(ctx, log, rp)
		if err != nil {
			errorResult = errors.Join(err, errorResult)
		}
		allResourcesToMigrate = append(allResourcesToMigrate, consoleResourcesToMigrate...)
	}

	for _, obj := range allResourcesToMigrate {
		err := r.Get(ctx, types.NamespacedName{
			Namespace: rp.Namespace,
			Name:      obj.resourceName,
		}, obj.resource)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("get %s (%s): %w", obj.helperString, obj.resourceName, err), errorResult)
		} else if !hasLabelsAndAnnotations(obj.resource, rp) {
			annotatedObject := obj.resource.DeepCopyObject()
			setHelmLabelsAndAnnotations(annotatedObject.(client.Object), rp)

			resourceName := annotatedObject.(client.Object).GetName()
			err = r.Update(ctx, annotatedObject.(client.Object))
			if err != nil {
				errorResult = errors.Join(fmt.Errorf("updating %s (%s): %w", obj.helperString, resourceName, err), errorResult)
			}

			msg := "update " + obj.helperString
			log.V(DebugLevel).Info(msg, "object-name", resourceName, "labels", annotatedObject.(client.Object).GetLabels(), "annotations", annotatedObject.(client.Object).GetAnnotations())
			r.EventRecorder.AnnotatedEventf(annotatedObject, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
		}
	}

	return errorResult
}

func (r *RedpandaReconciler) tryMigrateRedpanda(ctx context.Context, log logr.Logger, rp *v1alpha2.Redpanda) ([]resourceToMigrate, error) {
	errorResult := r.migrateRedpandaPods(ctx, log, rp)

	resourcesName := rp.Name
	if override := ptr.Deref(rp.Spec.ClusterSpec.FullnameOverride, ""); override != "" {
		resourcesName = override
	}

	var svc corev1.Service
	err := r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      resourcesName,
	}, &svc)
	if err != nil { // nolint:dupl // Repetition in tryMigration function is acceptable as generalised function would not bring any value
		errorResult = errors.Join(fmt.Errorf("get internal service (%s): %w", resourcesName, err), errorResult)
	} else if !hasLabelsAndAnnotations(&svc, rp) || !maps.Equal(svc.Spec.Selector, map[string]string{
		"app.kubernetes.io/instance": rp.Name,
		"app.kubernetes.io/name":     "redpanda",
	}) {
		internalService := svc.DeepCopy()
		setHelmLabelsAndAnnotations(internalService, rp)

		internalService.Spec.Selector = make(map[string]string)
		internalService.Spec.Selector["app.kubernetes.io/instance"] = rp.Name
		internalService.Spec.Selector["app.kubernetes.io/name"] = "redpanda"

		err = r.Update(ctx, internalService)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("updating internal service (%s): %w", internalService.Name, err), errorResult)
		}

		msg := "update internal Service"
		log.V(DebugLevel).Info(msg, "service-name", internalService.Name, "labels", internalService.Labels, "annotations", internalService.Annotations, "selector", internalService.Spec.Selector)
		r.EventRecorder.AnnotatedEventf(internalService, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}

	var sts appsv1.StatefulSet
	err = r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      resourcesName,
	}, &sts)
	if err != nil {
		errorResult = errors.Join(fmt.Errorf("get statefulset (%s): %w", resourcesName, err), errorResult)
	} else if !hasLabelsAndAnnotations(&sts, rp) {
		orphan := metav1.DeletePropagationOrphan
		err = r.Delete(ctx, &sts, &client.DeleteOptions{
			PropagationPolicy: &orphan,
		})
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("deleting statefulset (%s): %w", sts.Name, err), errorResult)
		}

		msg := "delete StatefulSet with orphant propagation mode"
		log.V(DebugLevel).Info(msg, "stateful-set-name", sts.Name)
		r.EventRecorder.AnnotatedEventf(&sts, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}
	return []resourceToMigrate{
		{fmt.Sprintf("%s-external", resourcesName), "external Service", &svc},
		{resourcesName, "ServiceAccount", &corev1.ServiceAccount{}},
		{resourcesName, "PodDistributionBudget", &policyv1.PodDisruptionBudget{}},
	}, errorResult
}

func (r *RedpandaReconciler) migrateRedpandaPods(ctx context.Context, log logr.Logger, rp *v1alpha2.Redpanda) error {
	var errorResult error

	pl, err := getPodList(ctx, r.Client, rp.Namespace, labels.Set{
		"app.kubernetes.io/instance": rp.Name,
		"app.kubernetes.io/name":     "redpanda",
	}.AsSelector())
	if err != nil {
		errorResult = errors.Join(fmt.Errorf("listing pods: %w", err), errorResult)
	}

	for i := range pl.Items {
		if l, exist := pl.Items[i].Labels["app.kubernetes.io/component"]; exist && l == componentLabelValue && !controllerutil.ContainsFinalizer(&pl.Items[i], FinalizerKey) {
			continue
		}
		newPod := pl.Items[i].DeepCopy()
		if newPod.Labels == nil {
			newPod.Labels = make(map[string]string)
		}
		newPod.Labels["app.kubernetes.io/component"] = componentLabelValue

		controllerutil.RemoveFinalizer(newPod, FinalizerKey)

		err = r.Update(ctx, newPod)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("updating component Pod label (%s): %w", newPod.Name, err), errorResult)
		}

		msg := "update Redpanda Pod"
		log.V(DebugLevel).Info(msg, "pod-name", newPod.Name, "labels", newPod.Labels, "finalizers", newPod.Finalizers)
		r.EventRecorder.AnnotatedEventf(newPod, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}
	return errorResult
}

func (r *RedpandaReconciler) tryMigrateConsole(ctx context.Context, log logr.Logger, rp *v1alpha2.Redpanda) ([]resourceToMigrate, error) {
	log.V(DebugLevel).Info("migrate console")
	consoleResourcesName := rp.Name
	if overwriteSAName := ptr.Deref(rp.Spec.ClusterSpec.Console.FullNameOverride, ""); overwriteSAName != "" {
		consoleResourcesName = overwriteSAName
	}

	var errorResult error

	var svc corev1.Service
	err := r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      consoleResourcesName,
	}, &svc)
	if err != nil { // nolint:dupl // Repetition in tryMigration function is acceptable as generalised function would not bring any value
		errorResult = errors.Join(fmt.Errorf("get console service (%s): %w", consoleResourcesName, err), errorResult)
	} else if !hasLabelsAndAnnotations(&svc, rp) || !maps.Equal(svc.Spec.Selector, map[string]string{
		"app.kubernetes.io/instance": rp.Name,
		"app.kubernetes.io/name":     "console",
	}) {
		annotatedConsoleSVC := svc.DeepCopy()
		setHelmLabelsAndAnnotations(annotatedConsoleSVC, rp)

		annotatedConsoleSVC.Spec.Selector = make(map[string]string)
		annotatedConsoleSVC.Spec.Selector["app.kubernetes.io/instance"] = rp.Name
		annotatedConsoleSVC.Spec.Selector["app.kubernetes.io/name"] = "console"

		err = r.Update(ctx, annotatedConsoleSVC)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("updating console service (%s): %w", annotatedConsoleSVC.Name, err), errorResult)
		}

		msg := "update console Service"
		log.V(DebugLevel).Info(msg, "service-name", annotatedConsoleSVC.Name, "labels", annotatedConsoleSVC.Labels, "annotations", annotatedConsoleSVC.Annotations, "selector", annotatedConsoleSVC.Spec.Selector)
		r.EventRecorder.AnnotatedEventf(annotatedConsoleSVC, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}

	var deploy appsv1.Deployment
	err = r.Get(ctx, types.NamespacedName{
		Namespace: rp.Namespace,
		Name:      consoleResourcesName,
	}, &deploy)
	if err != nil {
		errorResult = errors.Join(fmt.Errorf("get console deployment (%s): %w", consoleResourcesName, err), errorResult)
	} else if !hasLabelsAndAnnotations(&deploy, rp) {
		err = r.Delete(ctx, &deploy)
		if err != nil {
			errorResult = errors.Join(fmt.Errorf("deleting console deployment (%s): %w", deploy.Name, err), errorResult)
		}

		msg := "delete console Deployment"
		log.V(DebugLevel).Info(msg, "deployment-name", deploy.Name)
		r.EventRecorder.AnnotatedEventf(&deploy, map[string]string{v2.GroupVersion.Group + revisionPath: rp.Status.LastAttemptedRevision}, "Normal", v1alpha2.EventSeverityInfo, msg)
	}
	return []resourceToMigrate{
		{consoleResourcesName, "console ServiceAccount", &corev1.ServiceAccount{}},
		{consoleResourcesName, "console Ingress", &networkingv1.Ingress{}},
	}, errorResult
}

func hasLabelsAndAnnotations(object client.Object, rp *v1alpha2.Redpanda) bool {
	manageByLabel := false
	releaseName := false
	releaseNamespace := false
	for k, v := range object.GetLabels() {
		if k == "app.kubernetes.io/managed-by" && v == helm {
			manageByLabel = true
		}
	}

	for k, v := range object.GetAnnotations() {
		switch k {
		case "meta.helm.sh/release-name":
			releaseName = v == rp.Name
		case "meta.helm.sh/release-namespace":
			releaseNamespace = v == rp.Namespace
		}
	}

	return manageByLabel && releaseName && releaseNamespace
}

const helm = "Helm"

func setHelmLabelsAndAnnotations(object client.Object, rp *v1alpha2.Redpanda) {
	l := make(map[string]string)
	l["app.kubernetes.io/managed-by"] = helm
	object.SetLabels(l)

	annotations := make(map[string]string)
	annotations["meta.helm.sh/release-name"] = rp.Name
	annotations["meta.helm.sh/release-namespace"] = rp.Namespace
	object.SetAnnotations(annotations)
}

func asObj[T kube.Object](manifests []T) []kube.Object {
	out := make([]kube.Object, len(manifests))
	for i := range manifests {
		out[i] = manifests[i]
	}
	return out
}

func (r *RedpandaReconciler) getManifests(rp *v1alpha2.Redpanda) ([]kube.Object, error) {
	dot, err := rp.GetDot()
	if err != nil {
		return nil, err
	}

	dot.KubeConfig = r.kubeConfig

	// Template does not provide a way to pass kubeconfig which lookup needs.
	//redpanda.Template(release, partial)

	manifests := []kube.Object{
		redpanda.NodePortService(dot),
		redpanda.PodDisruptionBudget(dot),
		redpanda.ServiceAccount(dot),
		redpanda.ServiceInternal(dot),
		redpanda.ServiceMonitor(dot),
		redpanda.SidecarControllersRole(dot),
		redpanda.SidecarControllersRoleBinding(dot),
		redpanda.StatefulSet(dot),
		redpanda.PostUpgrade(dot),
		redpanda.PostInstallUpgradeJob(dot),
	}

	manifests = append(manifests, asObj(redpanda.ConfigMaps(dot))...)
	manifests = append(manifests, asObj(redpanda.CertIssuers(dot))...)
	manifests = append(manifests, asObj(redpanda.RootCAs(dot))...)
	manifests = append(manifests, asObj(redpanda.ClientCerts(dot))...)
	manifests = append(manifests, asObj(redpanda.ClusterRoleBindings(dot))...)
	manifests = append(manifests, asObj(redpanda.ClusterRoles(dot))...)
	manifests = append(manifests, asObj(redpanda.LoadBalancerServices(dot))...)
	manifests = append(manifests, asObj(redpanda.Secrets(dot))...)

	return manifests, nil
}

func (r *RedpandaReconciler) reconcile(ctx context.Context, rp *v1alpha2.Redpanda) (*v1alpha2.Redpanda, ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.reconcile")

	rp.Status.Failures = 0
	manifests, err := r.getManifests(rp)
	if err != nil {
		return nil, ctrl.Result{}, err
	}

	for _, resource := range manifests {
		// Nil unboxing issue
		if reflect.ValueOf(resource).IsNil() {
			continue
		}

		ob, err := convertToUnstructed(resource)
		if err != nil {
			log.Error(err, "unable to convert to unstructured resource", "resource", resource)
		}

		err = r.ServerSideApply(ctx, ob)
		if err != nil {
			// Do not stop creating resources. Even if one was unsuccessful.
			log.Error(err, "failed to create resource", "resource", resource)
			rp.Status.Failures++
		}
	}

	return v1alpha2.RedpandaReady(rp), ctrl.Result{}, nil
}

func convertToUnstructed(resource kube.Object) (*unstructured.Unstructured, error) {
	b, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	convertedObj := &unstructured.Unstructured{}
	err = json.Unmarshal(b, convertedObj)
	if err != nil {
		return nil, err
	}

	return convertedObj, nil
}

// based on https://github.com/fluxcd/pkg/blob/30c101fc7c9fac4d84937ff4890a3da46a9db2dd/ssa/manager_apply.go#L94-L140
func (r *RedpandaReconciler) ServerSideApply(ctx context.Context, resource *unstructured.Unstructured) error {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.reconcile.ServerSideApply")
	log.V(DebugLevel).Info("resource creation", "resource", resource)
	existingObject := &unstructured.Unstructured{}
	existingObject.SetGroupVersionKind(resource.GetObjectKind().GroupVersionKind())
	getError := r.Get(ctx, client.ObjectKeyFromObject(resource), existingObject)

	dryRunObject := resource.DeepCopy()
	if err := r.dryRunApply(ctx, dryRunObject); err != nil {
		if !k8serrors.IsNotFound(getError) && isImmutableError(err) {
			log.V(DebugLevel).Info("immutable error", "resource", resource)
			if err := r.Delete(ctx, existingObject, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !k8serrors.IsNotFound(err) {
				return err
			}
			return r.ServerSideApply(ctx, resource)
		}

		return err
	}

	// do not apply objects that have not drifted to avoid bumping the resource version
	if resource.GetResourceVersion() != "" &&
		apiequality.Semantic.DeepEqual(dryRunObject.GetLabels(), existingObject.GetLabels()) &&
		apiequality.Semantic.DeepEqual(dryRunObject.GetAnnotations(), existingObject.GetAnnotations()) &&
		apiequality.Semantic.DeepEqual(prepareObjectForDiff(dryRunObject), prepareObjectForDiff(existingObject)) {
		return nil
	}

	appliedObject := resource.DeepCopy()
	return r.apply(ctx, appliedObject)
}

// prepareObjectForDiff removes the metadata and status fields from the given object
func prepareObjectForDiff(object *unstructured.Unstructured) *unstructured.Unstructured {
	deepCopy := object.DeepCopy()
	unstructured.RemoveNestedField(deepCopy.Object, "metadata")
	unstructured.RemoveNestedField(deepCopy.Object, "status")
	return deepCopy
}

// Match CEL immutable error variants.
var matchImmutableFieldErrors = []*regexp.Regexp{
	regexp.MustCompile(`.*is\simmutable.*`),
	regexp.MustCompile(`.*immutable\sfield.*`),
}

func isImmutableError(err error) bool {
	// Detect immutability like kubectl does
	// https://github.com/kubernetes/kubectl/blob/8165f83007/pkg/cmd/apply/patcher.go#L201
	if k8serrors.IsConflict(err) || k8serrors.IsInvalid(err) {
		return true
	}

	// Detect immutable errors returned by custom admission webhooks and Kubernetes CEL
	// https://kubernetes.io/blog/2022/09/29/enforce-immutability-using-cel/#immutablility-after-first-modification
	for _, fieldError := range matchImmutableFieldErrors {
		if fieldError.MatchString(err.Error()) {
			return true
		}
	}

	return false
}

func (r *RedpandaReconciler) dryRunApply(ctx context.Context, object client.Object) error {
	opts := []client.PatchOption{
		client.DryRunAll,
		client.ForceOwnership,
		fieldOwner,
	}

	return r.Patch(ctx, object, client.Apply, opts...)
}

func (r *RedpandaReconciler) apply(ctx context.Context, object client.Object) error {
	opts := []client.PatchOption{
		client.ForceOwnership,
		fieldOwner,
	}
	return r.Patch(ctx, object, client.Apply, opts...)
}

func (r *RedpandaReconciler) reconcileDelete(ctx context.Context, rp *v1alpha2.Redpanda) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.reconcileDelete")

	manifests, err := r.getManifests(rp)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, resource := range manifests {
		// Nil unboxing issue
		if reflect.ValueOf(resource).IsNil() {
			continue
		}
		err := r.Delete(ctx, resource)
		if err != nil {
			// Do not stop deletion when one resource can not be deleted.
			// It might be the case where resource was already deleted.
			log.Error(err, "failed to delete resource", "resource", resource)
		}
	}

	patch := client.MergeFrom(rp.DeepCopy())
	controllerutil.RemoveFinalizer(rp, FinalizerKey)
	if err := r.Patch(ctx, rp, patch); err != nil {
		log.Error(err, "finalizer not removed")
	}

	return ctrl.Result{}, nil
}

func (r *RedpandaReconciler) reconcileStatus(ctx context.Context, rp *v1alpha2.Redpanda) error {
	log := ctrl.LoggerFrom(ctx).WithName("RedpandaReconciler.reconcileStatus")

	rp.Status.ObservedGeneration = rp.Generation

	dot, err := rp.GetDot()
	if err != nil {
		log.Error(err, "failed to get dot")
	}
	sel := redpanda.StatefulSetPodLabelsSelector(dot)

	// Get redpanda Pods readiness
	var pods corev1.PodList
	err = r.List(ctx, &pods, &client.ListOptions{
		Namespace:     rp.Namespace,
		LabelSelector: labels.SelectorFromValidatedSet(sel),
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"status.phase": string(corev1.PodRunning),
		}),
	})
	if err != nil {
		log.Error(err, "failed to list pods")
	}
	rpPodsNum := len(pods.Items)

	// TODO (Rafal) Make it a function within helm chart
	sel = helmette.Merge(map[string]string{
		"app.kubernetes.io/name":      redpanda.Name(dot),
		"app.kubernetes.io/instance":  dot.Release.Name,
		"app.kubernetes.io/component": fmt.Sprintf("%s-post-upgrade", helmette.Trunc(50, redpanda.Name(dot))),
	}, rp.Spec.ClusterSpec.CommonLabels)

	// Get Jobs completeness
	err = r.List(ctx, &pods, &client.ListOptions{
		Namespace:     rp.Namespace,
		LabelSelector: labels.SelectorFromValidatedSet(sel),
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"status.phase": string(corev1.PodSucceeded),
		}),
	})
	if err != nil {
		log.Error(err, "failed to list Jobs")
	}
	postUpgradeJobsNum := len(pods.Items)

	sel = helmette.Merge(map[string]string{
		"app.kubernetes.io/name":      redpanda.Name(dot),
		"app.kubernetes.io/instance":  dot.Release.Name,
		"app.kubernetes.io/component": fmt.Sprintf("%.50s-post-install", helmette.Trunc(50, redpanda.Name(dot))),
	}, rp.Spec.ClusterSpec.CommonLabels)

	err = r.List(ctx, &pods, &client.ListOptions{
		Namespace:     rp.Namespace,
		LabelSelector: labels.SelectorFromValidatedSet(sel),
		FieldSelector: fields.SelectorFromSet(fields.Set{
			"status.phase": string(corev1.PodSucceeded),
		}),
	})
	if err != nil {
		log.Error(err, "failed to list Jobs")
	}
	postInstallJobsNum := len(pods.Items)

	retry := false
	if postUpgradeJobsNum+postInstallJobsNum != 2 || rpPodsNum != ptr.Deref(rp.Spec.ClusterSpec.Statefulset.Replicas, 3) && rp.Status.Failures != 0 {
		// As controller does not
		retry = true
		v1alpha2.RedpandaNotReady(rp,
			"NoAllPodsAreReady",
			fmt.Sprintf("Redpanda Pods ready: %d vs Redpanda Pods required: %d; Post Install Jobs finished: %d Post Upgrade Jobs finished: %d vs Jobs required 2 to finish",
				len(pods.Items), ptr.Deref(rp.Spec.ClusterSpec.Statefulset.Replicas, 0),
				postInstallJobsNum, postUpgradeJobsNum))
	}

	rp.SetGroupVersionKind(v1alpha2.GroupVersion.WithKind("Redpanda"))
	rp.SetManagedFields(nil)

	err = r.Client.Status().Patch(ctx, rp, client.Apply, client.ForceOwnership, fieldOwner)
	if err != nil {
		return err
	}
	if retry {
		return errors.New("retry until Redpanda is ready")
	}
	return nil
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

func disableRedpandaReconciliation(redpandaCluster *vectorzied_v1alpha1.Cluster) {
	managedAnnotationKey := vectorzied_v1alpha1.GroupVersion.Group + managedPath
	if redpandaCluster.Annotations == nil {
		redpandaCluster.Annotations = map[string]string{}
	}
	redpandaCluster.Annotations[managedAnnotationKey] = NotManaged
}

func disableConsoleReconciliation(console *vectorzied_v1alpha1.Console) {
	managedAnnotationKey := vectorzied_v1alpha1.GroupVersion.Group + managedPath
	if console.Annotations == nil {
		console.Annotations = map[string]string{}
	}
	console.Annotations[managedAnnotationKey] = NotManaged
}
