// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package pipeline implements the controller for the Pipeline CRD.
package pipeline

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/license"
	"github.com/redpanda-data/common-go/otelutil/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	finalizerKey = "operator.redpanda.com/finalizer"

	// clusterRefIndexField is the field index key for looking up Pipelines
	// that reference a given Redpanda cluster.
	clusterRefIndexField = "spec.cluster.clusterRef.name"
)

// MonitoringConfig holds the operator-level monitoring settings for Connect pipelines.
type MonitoringConfig struct {
	Enabled        bool
	ScrapeInterval string
	Labels         map[string]string
}

// Controller reconciles Pipeline resources.
type Controller struct {
	Ctl       *kube.Ctl
	namespace string
	// LicenseFilePath is the path to the operator-level enterprise license file,
	// configured via enterprise.licenseSecretRef in the operator Helm chart values.
	LicenseFilePath string
	// CommonAnnotations are annotations from the operator Helm chart values
	// that are propagated to all resources managed by the operator.
	CommonAnnotations map[string]string
	// DefaultImage is the operator-level Redpanda Connect image override. When
	// set, it's used as the fallback for Pipeline CRs that don't pin their own
	// .spec.image. Per-Pipeline .spec.image still wins. When empty, falls
	// through to the binary's hardcoded PipelineDefaultImage constant.
	//
	// Plumbed from the operator chart's connectController.image.{repository,tag}
	// values via the --connect-default-image flag.
	DefaultImage string
	// Monitoring holds the operator-level monitoring configuration for Connect pipelines.
	Monitoring MonitoringConfig
	// MaxConcurrentReconciles bounds how many pipelines reconcile in parallel.
	// 0 falls back to defaultMaxConcurrentReconciles. Higher values speed up
	// convergence of large pipeline fleets (e.g. mass create / operator restart)
	// at the cost of more concurrent API + render work.
	MaxConcurrentReconciles int

	// Disabled turns the Connect controller off entirely: SetupWithManager
	// registers nothing (no Pipeline watches, no field index) and returns
	// immediately. It exists for library consumers that construct the
	// Controller unconditionally; the operator's own cmd/run entrypoint
	// instead skips SetupWithManager altogether when --enable-connect is
	// false (the chart's connectController.enabled value) and never sets
	// this field. Note the operator's central telemetry collector reports
	// Pipeline CRs regardless of whether this controller runs.
	//
	// Disabling does NOT touch existing workloads: pipeline Deployments and
	// their pods are owned by the Pipeline CRs, not by the operator, so they
	// keep running (and keep processing data) unmanaged. Note that Pipeline
	// CRs carry an operator finalizer, so deleting a Pipeline while the
	// controller is disabled leaves it in Terminating until the controller
	// is re-enabled (or the finalizer is removed by hand). To stop using
	// Connect cleanly, delete the Pipeline CRs first, then disable.
	Disabled bool

	// clusterConns memoizes the expensive clusterRef chart render per Redpanda
	// cluster (keyed by spec generation) so many pipelines sharing one cluster
	// don't each re-render it. Initialized in SetupWithManager.
	clusterConns *clusterConnCache
}

// defaultMaxConcurrentReconciles is the parallelism used when the Controller's
// MaxConcurrentReconciles is unset. Pipelines are independent, so modest
// parallelism converges large fleets quickly without overwhelming the API.
const defaultMaxConcurrentReconciles = 5

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=redpandas,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=users,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete

func (c *Controller) SetupWithManager(ctx context.Context, mgr ctrl.Manager, namespace string) error {
	if c.Disabled {
		return nil
	}

	c.namespace = namespace
	c.clusterConns = newClusterConnCache()

	// Register the upstream Redpanda CR types on the manager scheme so we can
	// watch them below. We register only Redpanda/RedpandaList (not the OSS
	// AddToScheme, which would also register User and collide with the
	// enterprise User type on the shared cluster.redpanda.com/v1alpha2 GVK).
	mgr.GetScheme().AddKnownTypes(
		schema.GroupVersion{Group: "cluster.redpanda.com", Version: "v1alpha2"},
		&redpandav1alpha2.Redpanda{}, &redpandav1alpha2.RedpandaList{},
	)

	// Index Pipelines by their clusterRef name so we can efficiently look up
	// which Pipelines reference a given Redpanda cluster.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &redpandav1alpha2.Pipeline{}, clusterRefIndexField, func(o client.Object) []string {
		pipeline := o.(*redpandav1alpha2.Pipeline)
		if pipeline.Spec.ClusterSource != nil && pipeline.Spec.ClusterSource.ClusterRef != nil {
			return []string{pipeline.Spec.ClusterSource.ClusterRef.Name}
		}
		return nil
	}); err != nil {
		return err
	}

	maxConcurrent := c.MaxConcurrentReconciles
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentReconciles
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: maxConcurrent}).
		For(&redpandav1alpha2.Pipeline{})

	for _, t := range Types() {
		// Skip the PodMonitor watch if the CRD is not installed. If it gets
		// installed during operator runtime, the operator must be restarted.
		if _, ok := t.(*monitoringv1.PodMonitor); ok {
			if !c.podMonitorCRDInstalled(ctx, mgr) {
				continue
			}
		}
		builder = builder.Owns(t)
	}

	// Watch Redpanda clusters and re-reconcile any Pipelines that reference
	// them. This ensures Pipelines pick up broker/TLS changes promptly. On
	// cluster deletion, additionally evict the cluster's render-cache entry
	// so the cache doesn't grow unboundedly (and a recreate under the same
	// name starts clean).
	enqueueForCluster := func(ctx context.Context, o client.Object, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		var pipelineList redpandav1alpha2.PipelineList
		if err := mgr.GetClient().List(ctx, &pipelineList,
			client.InNamespace(o.GetNamespace()),
			client.MatchingFields{clusterRefIndexField: o.GetName()},
		); err != nil {
			return
		}
		for i := range pipelineList.Items {
			q.Add(reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&pipelineList.Items[i]),
			})
		}
	}
	builder = builder.Watches(&redpandav1alpha2.Redpanda{}, handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueForCluster(ctx, e.Object, q)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueForCluster(ctx, e.ObjectNew, q)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			c.clusterConns.evict(e.Object.GetNamespace(), e.Object.GetName())
			enqueueForCluster(ctx, e.Object, q)
		},
		GenericFunc: func(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueueForCluster(ctx, e.Object, q)
		},
	})

	return builder.Complete(c)
}

// podMonitorCRDInstalled reports whether the monitoring.coreos.com PodMonitor
// CRD is registered with the API server. It consults the manager's RESTMapper
// (backed by discovery) instead of the cached client: SetupWithManager runs
// before mgr.Start(), where a cached List always fails with ErrCacheNotStarted
// — which would make the probe skip the PodMonitor watch on every deployment
// regardless of whether the CRD is present.
func (c *Controller) podMonitorCRDInstalled(ctx context.Context, mgr ctrl.Manager) bool {
	gvk := monitoringv1.SchemeGroupVersion.WithKind(monitoringv1.PodMonitorsKind)
	if _, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version); err != nil {
		if !meta.IsNoMatchError(err) {
			log.Error(ctx, err, "could not determine whether the PodMonitor CRD is installed; skipping the PodMonitor watch")
		}
		return false
	}
	return true
}

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if c.namespace != "" && req.Namespace != c.namespace {
		return ctrl.Result{}, nil
	}

	pipeline, err := kube.Get[redpandav1alpha2.Pipeline](ctx, c.Ctl, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion: clean up owned resources and remove finalizer.
	if !pipeline.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(pipeline, finalizerKey) {
			syncer, err := c.syncerFor(pipeline, c.rendererFor(pipeline, nil, nil, nil, ""))
			if err != nil {
				return ctrl.Result{}, err
			}
			if _, err := syncer.DeleteAll(ctx); err != nil {
				return ctrl.Result{}, err
			}
			// NB: Apply can't be used to remove finalizers.
			if err := c.Ctl.Update(ctx, pipeline); err != nil {
				// The object may have been fully deleted between Get and
				// Update. This is benign — the finalizer is already gone.
				if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if missing. Use Update (not Apply/SSA) to avoid taking
	// ownership of spec fields, which would conflict with user-side SSA.
	if controllerutil.AddFinalizer(pipeline, finalizerKey) {
		if err := c.Ctl.Update(ctx, pipeline); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve cluster connection details if clusterRef is set.
	clusterConn, err := c.resolveClusterSource(ctx, pipeline)
	if err != nil {
		log.Error(ctx, err, "failed to resolve clusterRef")
		if statusErr := c.failStatus(ctx, pipeline, redpandav1alpha2.PipelineConditionClusterRef, redpandav1alpha2.PipelineReasonClusterRefInvalid, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		// Deliberately leave any previously-synced children (Deployment,
		// ConfigMap, ...) in place: a resolution failure here may be
		// transient (an API hiccup, an operator restart racing informer
		// warm-up), and tearing down a running pipeline would stop data
		// processing. The last-known-good config keeps running until the
		// ref resolves again or the Pipeline CR is deleted.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Resolve the named SCRAM user when .userRef is set. The pipeline
	// authenticates as this identity instead of using the cluster's
	// bootstrap superuser.
	userCreds, err := resolveUserRef(ctx, c.Ctl, pipeline)
	if err != nil {
		log.Error(ctx, err, "failed to resolve userRef")
		if statusErr := c.failStatus(ctx, pipeline, redpandav1alpha2.PipelineConditionUserRef, redpandav1alpha2.PipelineReasonUserInvalid, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		// As with clusterRef above: keep existing children running on a
		// userRef resolution failure rather than tearing down the workload.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Validate that every spec.valueSources entry resolves to an existing
	// backing object + key. Without this, a typo'd Secret name would sync
	// fine and only surface later as a pod stuck in
	// CreateContainerConfigError — and, with the Recreate strategy, take the
	// previously-running pods down with it.
	if err := validateValueSources(ctx, c.Ctl, pipeline); err != nil {
		log.Error(ctx, err, "failed to resolve valueSources")
		if statusErr := c.failStatus(ctx, pipeline, redpandav1alpha2.PipelineConditionValueSourcesResolved, redpandav1alpha2.PipelineReasonValueSourceInvalid, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		// Keep existing children running; the referenced object may appear
		// shortly (e.g. an external-secrets materialization in flight).
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Validate license before proceeding.
	if err := c.validateLicense(); err != nil {
		log.Error(ctx, err, "license validation failed")
		if statusErr := c.failStatus(ctx, pipeline, redpandav1alpha2.PipelineConditionLicense, redpandav1alpha2.PipelineReasonLicenseInvalid, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		// The license gate blocks syncing (a new Pipeline never gets children;
		// an existing one stops receiving spec updates) but does NOT tear down
		// already-running workloads: a license Secret that is briefly
		// unreadable — or an expiry over a weekend — must not stop data
		// processing. The Connect runtime enforces its own license gate for
		// enterprise components whenever pods restart.
		// Requeue to retry the license check periodically.
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Read the raw license bytes so the renderer can mirror them into a
	// Pipeline-owned Secret. The Connect runtime has its own enterprise-license
	// gate (independent of the operator's gate above), and reading the
	// REDPANDA_LICENSE env var from this Secret unblocks enterprise inputs
	// like mysql_cdc without forcing the user to wire the license up twice.
	licenseContent, err := os.ReadFile(c.LicenseFilePath)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "reading license file for pipeline pod")
	}

	// Digest the referenced credentials (Secret/ConfigMap resourceVersions +
	// license bytes) so rotations roll the Deployment. Computed from object
	// metadata, not secret contents, to avoid exposing content-derived
	// digests on the pod template.
	credsChecksum, err := c.credentialsChecksum(ctx, pipeline, clusterConn, userCreds, licenseContent)
	if err != nil {
		return ctrl.Result{}, err
	}

	renderer := c.rendererFor(pipeline, clusterConn, userCreds, licenseContent, credsChecksum)

	// Refuse to adopt pre-existing objects this Pipeline didn't create: the
	// SSA sync below force-applies, so without this check a Pipeline named
	// after another workload's ConfigMap/Secret/Deployment would hijack that
	// object — and deleting the Pipeline would then delete it.
	if conflict, err := c.findOwnershipConflict(ctx, pipeline, renderer); err != nil {
		return ctrl.Result{}, err
	} else if conflict != "" {
		log.Info(ctx, "refusing to adopt conflicting resources", "conflict", conflict)
		phase, _ := c.liveStatus(ctx, pipeline, redpandav1alpha2.PipelineReasonNameConflict, conflict)
		if statusErr := c.applyStatus(ctx, pipeline, phase, []metav1.Condition{{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  redpandav1alpha2.PipelineReasonNameConflict,
			Message: conflict,
		}}); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Sync all child resources (ConfigMap, Deployment, license Secret) via SSA.
	syncer, err := c.syncerFor(pipeline, renderer)
	if err != nil {
		return ctrl.Result{}, err
	}

	objs, err := syncer.Sync(ctx)
	if err != nil {
		log.Error(ctx, err, "failed to sync resources")
		if statusErr := c.applyStatus(ctx, pipeline, redpandav1alpha2.PipelinePhaseUnknown, []metav1.Condition{{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  redpandav1alpha2.PipelineReasonFailed,
			Message: FormatConditionMessage("Resource", err.Error()),
		}}); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Derive status from the synced Deployment.
	phase, conditions := c.deriveStatus(ctx, pipeline, objs)

	// Add ClusterRef condition when a cluster source is configured.
	if clusterConn != nil {
		conditions = append(conditions, metav1.Condition{
			Type:    redpandav1alpha2.PipelineConditionClusterRef,
			Status:  metav1.ConditionTrue,
			Reason:  redpandav1alpha2.PipelineReasonClusterRefResolved,
			Message: "Cluster connection resolved successfully",
		})
	}

	// Add UserRef condition when a userRef is configured.
	if userCreds != nil {
		conditions = append(conditions, metav1.Condition{
			Type:    redpandav1alpha2.PipelineConditionUserRef,
			Status:  metav1.ConditionTrue,
			Reason:  redpandav1alpha2.PipelineReasonUserResolved,
			Message: fmt.Sprintf("Resolved SCRAM user %q with mechanism %s", userCreds.Username, userCreds.Mechanism),
		})
	}

	// Add ValueSourcesResolved when the spec declares valueSources.
	if len(pipeline.Spec.ValueSources) > 0 {
		conditions = append(conditions, metav1.Condition{
			Type:    redpandav1alpha2.PipelineConditionValueSourcesResolved,
			Status:  metav1.ConditionTrue,
			Reason:  redpandav1alpha2.PipelineReasonValueSourcesResolved,
			Message: "All valueSources entries resolved",
		})
	}

	// The license gate above passed for this reconcile.
	conditions = append(conditions, metav1.Condition{
		Type:    redpandav1alpha2.PipelineConditionLicense,
		Status:  metav1.ConditionTrue,
		Reason:  redpandav1alpha2.PipelineReasonLicenseValid,
		Message: "Enterprise license is valid and includes Redpanda Connect",
	})

	if err := c.applyStatus(ctx, pipeline, phase, conditions); err != nil {
		return ctrl.Result{}, err
	}

	// Use a shorter requeue when the pipeline is not yet running so we can
	// detect init-container lint failures quickly. Once running, use a
	// longer interval for periodic drift detection.
	requeueAfter := 5 * time.Minute
	if phase != redpandav1alpha2.PipelinePhaseRunning && phase != redpandav1alpha2.PipelinePhaseStopped {
		requeueAfter = 15 * time.Second
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (c *Controller) rendererFor(pipeline *redpandav1alpha2.Pipeline, clusterConn *clusterConnection, userCreds *userCredentials, licenseContent []byte, credentialsChecksum string) *render {
	return &render{
		pipeline:            pipeline,
		labels:              Labels(pipeline),
		commonAnnotations:   c.CommonAnnotations,
		defaultImage:        c.DefaultImage,
		monitoring:          c.Monitoring,
		clusterConn:         clusterConn,
		userCredentials:     userCreds,
		licenseContent:      licenseContent,
		credentialsChecksum: credentialsChecksum,
	}
}

func (c *Controller) syncerFor(pipeline *redpandav1alpha2.Pipeline, renderer *render) (*kube.Syncer, error) {
	gvk, err := kube.GVKFor(c.Ctl.Scheme(), pipeline)
	if err != nil {
		return nil, err
	}

	return &kube.Syncer{
		Ctl:             c.Ctl,
		Namespace:       pipeline.Namespace,
		Renderer:        renderer,
		Owner:           *metav1.NewControllerRef(pipeline, gvk),
		OwnershipLabels: renderer.labels,
	}, nil
}

// findOwnershipConflict renders the objects this Pipeline would sync and
// reports the first that already exists WITHOUT an owner reference to the
// Pipeline. The SSA sync uses ForceOwnership, so applying over such an object
// would adopt it (and Pipeline deletion would then delete it); refusing up
// front turns a would-be hijack of an unrelated ConfigMap/Secret/Deployment
// into a NameConflict status instead. Returns a human-readable description of
// the conflict, or "" when every object is safe to apply.
func (c *Controller) findOwnershipConflict(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, renderer *render) (string, error) {
	objs, err := renderer.Render(ctx)
	if err != nil {
		return "", err
	}

	for _, obj := range objs {
		existing, ok := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(kube.Object)
		if !ok {
			return "", errors.Newf("rendered object %T is not a kube.Object", obj)
		}
		if err := c.Ctl.Get(ctx, kube.AsKey(obj), existing); err != nil {
			// Not existing yet is the common case; an uninstalled CRD
			// (PodMonitor) is tolerated the same way the syncer tolerates it.
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				continue
			}
			return "", err
		}

		owned := slices.ContainsFunc(existing.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
			return ref.UID == pipeline.UID
		})
		if !owned {
			gvk, err := kube.GVKFor(c.Ctl.Scheme(), obj)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %q already exists and is not owned by this Pipeline; refusing to adopt it — rename the Pipeline or remove the conflicting object", gvk.Kind, kube.AsKey(obj).String()), nil
		}
	}

	return "", nil
}

func (c *Controller) validateLicense() error {
	if c.LicenseFilePath == "" {
		return errors.New("no license configured: set enterprise.licenseSecretRef in the operator Helm chart values")
	}

	l, err := license.ReadLicense(c.LicenseFilePath)
	if err != nil {
		return errors.Wrap(err, "failed to read license")
	}

	if err := license.CheckExpiration(l.Expires()); err != nil {
		return errors.Wrap(err, "license expired")
	}

	if !l.AllowsEnterpriseFeatures() {
		return errors.New("license does not allow enterprise features")
	}

	if !l.IncludesProduct(license.ProductConnect) {
		return errors.New("license does not include Redpanda Connect")
	}

	return nil
}

// deriveStatus examines the synced objects to determine the Pipeline phase and
// conditions. It returns both a Ready condition and, when applicable, a
// ConfigValid condition based on the lint init container status.
func (c *Controller) deriveStatus(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, objs []kube.Object) (redpandav1alpha2.PipelinePhase, []metav1.Condition) {
	for _, obj := range objs {
		if dp, ok := obj.(*appsv1.Deployment); ok {
			return c.statusFromDeployment(ctx, pipeline, dp)
		}
	}

	// Deployment not yet observed in sync results.
	pipeline.Status.Replicas = 0
	pipeline.Status.ReadyReplicas = 0
	return redpandav1alpha2.PipelinePhasePending, []metav1.Condition{{
		Type:    redpandav1alpha2.PipelineConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  redpandav1alpha2.PipelineReasonProvisioning,
		Message: "Deployment not yet created",
	}}
}

// statusFromDeployment maps a Deployment's observed state onto the Pipeline
// phase and workload conditions (Ready and, when relevant, ConfigValid).
func (c *Controller) statusFromDeployment(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, dp *appsv1.Deployment) (redpandav1alpha2.PipelinePhase, []metav1.Condition) {
	pipeline.Status.Replicas = dp.Status.Replicas
	pipeline.Status.ReadyReplicas = dp.Status.ReadyReplicas

	switch {
	case pipeline.GetReplicas() == 0:
		// Both spec.paused and an explicit replicas: 0 park the pipeline in
		// Stopped — the workload is intentionally scaled to zero either way.
		msg := "Pipeline is scaled to zero"
		if pipeline.Spec.Paused {
			msg = "Pipeline is paused"
		}
		return redpandav1alpha2.PipelinePhaseStopped, []metav1.Condition{{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionTrue,
			Reason:  redpandav1alpha2.PipelineReasonPaused,
			Message: msg,
		}}
	case dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0:
		return redpandav1alpha2.PipelinePhaseRunning, []metav1.Condition{
			{
				Type:    redpandav1alpha2.PipelineConditionReady,
				Status:  metav1.ConditionTrue,
				Reason:  redpandav1alpha2.PipelineReasonRunning,
				Message: "Pipeline is running",
			},
			{
				Type:    redpandav1alpha2.PipelineConditionConfigValid,
				Status:  metav1.ConditionTrue,
				Reason:  redpandav1alpha2.PipelineReasonConfigValid,
				Message: "Configuration passed lint validation",
			},
		}
	default:
		conditions := []metav1.Condition{{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  redpandav1alpha2.PipelineReasonProvisioning,
			Message: "Pipeline is starting up",
		}}

		healthy, msg := c.checkInitContainerStatus(ctx, dp)
		if !healthy {
			conditions = append(conditions, metav1.Condition{
				Type:    redpandav1alpha2.PipelineConditionConfigValid,
				Status:  metav1.ConditionFalse,
				Reason:  redpandav1alpha2.PipelineReasonConfigInvalid,
				Message: msg,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:    redpandav1alpha2.PipelineConditionConfigValid,
				Status:  metav1.ConditionTrue,
				Reason:  redpandav1alpha2.PipelineReasonConfigValid,
				Message: "Configuration passed lint validation",
			})
		}
		return redpandav1alpha2.PipelinePhaseProvisioning, conditions
	}
}

// liveStatus derives the Pipeline phase and workload conditions from the live
// Deployment rather than from sync results. Used on paths that don't sync
// (resolution failures, license failures, name conflicts) so a transient
// failure never misreports a still-running workload as Pending: the workload
// keeps processing data, and only the failing condition flips False. When the
// Deployment doesn't exist yet, the failure itself is reported as the Ready
// reason so a never-provisioned pipeline points straight at the cause.
func (c *Controller) liveStatus(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, fallbackReason, fallbackMessage string) (redpandav1alpha2.PipelinePhase, []metav1.Condition) {
	var dp appsv1.Deployment
	if err := c.Ctl.Get(ctx, kube.ObjectKey{Name: pipeline.Name, Namespace: pipeline.Namespace}, &dp); err != nil {
		pipeline.Status.Replicas = 0
		pipeline.Status.ReadyReplicas = 0
		return redpandav1alpha2.PipelinePhasePending, []metav1.Condition{{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  fallbackReason,
			Message: fallbackMessage,
		}}
	}
	return c.statusFromDeployment(ctx, pipeline, &dp)
}

// failStatus reports a resolution/validation failure on the Pipeline status
// without touching its children: phase and workload conditions come from the
// live Deployment (a Running pipeline stays Running — its data flow is
// unaffected), and the failing condition is overlaid on top.
func (c *Controller) failStatus(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, conditionType, reason, message string) error {
	phase, conditions := c.liveStatus(ctx, pipeline, reason, message)
	conditions = append(conditions, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	return c.applyStatus(ctx, pipeline, phase, conditions)
}

// maxConditionMessageLength bounds messages copied into status conditions.
// Lint failures inherit the init container's termination message, which (via
// terminationMessagePolicy: FallbackToLogsOnError) is a tail of the container
// log. Cap what gets copied into the API object — both for size and to limit
// how much raw log content (which runs with credentials in its environment)
// becomes readable to anyone with pipelines/get but no pod-log access.
const maxConditionMessageLength = 1024

// truncateConditionMessage caps msg at maxConditionMessageLength bytes,
// trimming any partial trailing UTF-8 rune so the result stays API-legal.
func truncateConditionMessage(msg string) string {
	if len(msg) <= maxConditionMessageLength {
		return msg
	}
	return strings.ToValidUTF8(msg[:maxConditionMessageLength], "") + "… (truncated; see the lint container logs for the full output)"
}

// checkInitContainerStatus lists pods for the given Deployment and checks
// whether the lint init container has failed. Returns (healthy, message).
func (c *Controller) checkInitContainerStatus(ctx context.Context, dp *appsv1.Deployment) (bool, string) {
	var podList corev1.PodList
	if err := c.Ctl.List(ctx, dp.Namespace, &podList, client.MatchingLabels(dp.Spec.Selector.MatchLabels)); err != nil {
		// If we can't list pods, don't block — assume healthy.
		return true, ""
	}

	for i := range podList.Items {
		for _, initStatus := range podList.Items[i].Status.InitContainerStatuses {
			if initStatus.Name != "lint" {
				continue
			}
			if initStatus.State.Terminated != nil && initStatus.State.Terminated.ExitCode != 0 {
				msg := initStatus.State.Terminated.Message
				if msg == "" {
					msg = fmt.Sprintf("lint exited with code %d", initStatus.State.Terminated.ExitCode)
				}
				return false, truncateConditionMessage(msg)
			}
			if initStatus.State.Waiting != nil && initStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				msg := initStatus.State.Waiting.Message
				if msg == "" {
					msg = "lint init container is in CrashLoopBackOff — check pipeline configuration"
				}
				return false, truncateConditionMessage(msg)
			}
			// Also check LastTerminationState: between restarts the current
			// State may be Running or Waiting (not yet CrashLoopBackOff),
			// but LastTerminationState records the previous failure.
			if initStatus.LastTerminationState.Terminated != nil && initStatus.LastTerminationState.Terminated.ExitCode != 0 {
				msg := initStatus.LastTerminationState.Terminated.Message
				if msg == "" {
					msg = fmt.Sprintf("lint exited with code %d", initStatus.LastTerminationState.Terminated.ExitCode)
				}
				return false, truncateConditionMessage(msg)
			}
		}
	}
	return true, ""
}

// applyStatus uses server-side apply to update the Pipeline status sub-resource.
func (c *Controller) applyStatus(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, phase redpandav1alpha2.PipelinePhase, conditions []metav1.Condition) error {
	// Drop conditions whose spec fields are no longer set — otherwise a
	// removed clusterRef/userRef/valueSources leaves its last condition
	// dangling on the status forever.
	if pipeline.Spec.ClusterSource == nil {
		meta.RemoveStatusCondition(&pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionClusterRef)
	}
	if pipeline.Spec.UserRef == nil {
		meta.RemoveStatusCondition(&pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionUserRef)
	}
	if len(pipeline.Spec.ValueSources) == 0 {
		meta.RemoveStatusCondition(&pipeline.Status.Conditions, redpandav1alpha2.PipelineConditionValueSourcesResolved)
	}

	pipeline.Status.ObservedGeneration = pipeline.Generation
	pipeline.Status.Phase = phase
	// status.selector backs the scale subresource's selectorpath (how HPA and
	// KEDA find the pods behind this Pipeline); it must stay in sync with the
	// Deployment's pod selector, which is fixed to Labels(pipeline).
	pipeline.Status.Selector = metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: Labels(pipeline)})
	pipeline.Status.Conditions = conditionsForApply(pipeline, conditions)

	return c.Ctl.ApplyStatus(ctx, pipeline, client.ForceOwnership)
}

// conditionsForApply merges the desired conditions into the existing conditions
// using the SSA-compatible helper from utils.
func conditionsForApply(pipeline *redpandav1alpha2.Pipeline, conditions []metav1.Condition) []metav1.Condition {
	merged := append([]metav1.Condition(nil), pipeline.Status.Conditions...)
	for i := range conditions {
		cond := conditions[i]
		cond.ObservedGeneration = pipeline.Generation
		meta.SetStatusCondition(&merged, cond)
	}
	return merged
}
