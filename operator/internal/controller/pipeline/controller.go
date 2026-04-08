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
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/license"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

const (
	FinalizerKey = "pipeline.redpanda.com/finalizer"
)

// MonitoringConfig holds the operator-level monitoring settings for Connect pipelines.
type MonitoringConfig struct {
	Enabled        bool
	ScrapeInterval string
	Labels         map[string]string
}

// Controller reconciles Pipeline resources.
type Controller struct {
	Ctl *kube.Ctl
	// LicenseFilePath is the path to the operator-level enterprise license file,
	// configured via enterprise.licenseSecretRef in the operator Helm chart values.
	LicenseFilePath string
	// CommonAnnotations are annotations from the operator Helm chart values
	// that are propagated to all resources managed by the operator.
	CommonAnnotations map[string]string
	// Monitoring holds the operator-level monitoring configuration for Connect pipelines.
	Monitoring MonitoringConfig
}

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete

func (c *Controller) SetupWithManager(ctx context.Context, mgr ctrl.Manager, namespace string) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Pipeline{}).
		Owns(&appsv1.Deployment{})

	return builder.Complete(c)
}

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("pipeline")

	pipeline, err := kube.Get[redpandav1alpha2.Pipeline](ctx, c.Ctl, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion: clean up owned resources and remove finalizer.
	if !pipeline.DeletionTimestamp.IsZero() {
		if controllerutil.RemoveFinalizer(pipeline, FinalizerKey) {
			syncer, err := c.syncerFor(pipeline)
			if err != nil {
				return ctrl.Result{}, err
			}
			if _, err := syncer.DeleteAll(ctx); err != nil {
				return ctrl.Result{}, err
			}
			// NB: Apply can't be used to remove finalizers.
			if err := c.Ctl.Update(ctx, pipeline); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if missing.
	if controllerutil.AddFinalizer(pipeline, FinalizerKey) {
		if err := c.Ctl.Apply(ctx, pipeline, client.ForceOwnership); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate license before proceeding.
	if err := c.validateLicense(); err != nil {
		logger.Error(err, "license validation failed")
		if statusErr := c.applyStatus(ctx, pipeline, redpandav1alpha2.PipelinePhasePending, metav1.Condition{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  redpandav1alpha2.PipelineReasonLicenseInvalid,
			Message: err.Error(),
		}); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		// Requeue to retry license check periodically.
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Sync all child resources (ConfigMap, Deployment) via SSA.
	syncer, err := c.syncerFor(pipeline)
	if err != nil {
		return ctrl.Result{}, err
	}

	objs, err := syncer.Sync(ctx)
	if err != nil {
		logger.Error(err, "failed to sync resources")
		if statusErr := c.applyStatus(ctx, pipeline, redpandav1alpha2.PipelinePhaseUnknown, metav1.Condition{
			Type:    redpandav1alpha2.PipelineConditionReady,
			Status:  metav1.ConditionFalse,
			Reason:  redpandav1alpha2.PipelineReasonFailed,
			Message: FormatConditionMessage("Resource", err.Error()),
		}); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err
	}

	// Derive status from the synced Deployment.
	phase, condition := c.deriveStatus(pipeline, objs)
	if err := c.applyStatus(ctx, pipeline, phase, condition); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (c *Controller) syncerFor(pipeline *redpandav1alpha2.Pipeline) (*kube.Syncer, error) {
	gvk, err := kube.GVKFor(c.Ctl.Scheme(), pipeline)
	if err != nil {
		return nil, err
	}

	labels := Labels(pipeline)

	return &kube.Syncer{
		Ctl:       c.Ctl,
		Namespace: pipeline.Namespace,
		Renderer: &render{
			pipeline:          pipeline,
			labels:            labels,
			commonAnnotations: c.CommonAnnotations,
			monitoring:        c.Monitoring,
		},
		Owner:           *metav1.NewControllerRef(pipeline, gvk),
		OwnershipLabels: labels,
	}, nil
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
// Ready condition.
func (c *Controller) deriveStatus(pipeline *redpandav1alpha2.Pipeline, objs []kube.Object) (redpandav1alpha2.PipelinePhase, metav1.Condition) {
	for _, obj := range objs {
		dp, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}

		pipeline.Status.Replicas = dp.Status.Replicas
		pipeline.Status.ReadyReplicas = dp.Status.ReadyReplicas

		switch {
		case pipeline.Spec.Paused:
			return redpandav1alpha2.PipelinePhaseStopped, metav1.Condition{
				Type:    redpandav1alpha2.PipelineConditionReady,
				Status:  metav1.ConditionTrue,
				Reason:  redpandav1alpha2.PipelineReasonPaused,
				Message: "Pipeline is paused",
			}
		case dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0:
			return redpandav1alpha2.PipelinePhaseRunning, metav1.Condition{
				Type:    redpandav1alpha2.PipelineConditionReady,
				Status:  metav1.ConditionTrue,
				Reason:  redpandav1alpha2.PipelineReasonRunning,
				Message: "Pipeline is running",
			}
		default:
			return redpandav1alpha2.PipelinePhaseProvisioning, metav1.Condition{
				Type:    redpandav1alpha2.PipelineConditionReady,
				Status:  metav1.ConditionFalse,
				Reason:  redpandav1alpha2.PipelineReasonProvisioning,
				Message: "Pipeline is starting up",
			}
		}
	}

	// Deployment not yet observed in sync results.
	pipeline.Status.Replicas = 0
	pipeline.Status.ReadyReplicas = 0
	return redpandav1alpha2.PipelinePhasePending, metav1.Condition{
		Type:    redpandav1alpha2.PipelineConditionReady,
		Status:  metav1.ConditionFalse,
		Reason:  redpandav1alpha2.PipelineReasonProvisioning,
		Message: "Deployment not yet created",
	}
}

// applyStatus uses server-side apply to update the Pipeline status sub-resource.
func (c *Controller) applyStatus(ctx context.Context, pipeline *redpandav1alpha2.Pipeline, phase redpandav1alpha2.PipelinePhase, condition metav1.Condition) error {
	pipeline.Status.ObservedGeneration = pipeline.Generation
	pipeline.Status.Phase = phase
	pipeline.Status.Conditions = conditionsForApply(pipeline, condition)

	return c.Ctl.ApplyStatus(ctx, pipeline, client.ForceOwnership)
}

// conditionsForApply merges the desired condition into the existing conditions
// using the SSA-compatible helper from utils.
func conditionsForApply(pipeline *redpandav1alpha2.Pipeline, condition metav1.Condition) []metav1.Condition {
	configs := utils.StatusConditionConfigs(pipeline.Status.Conditions, pipeline.Generation, []metav1.Condition{condition})

	out := make([]metav1.Condition, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, metav1.Condition{
			Type:               *cfg.Type,
			Status:             *cfg.Status,
			Reason:             *cfg.Reason,
			Message:            *cfg.Message,
			ObservedGeneration: *cfg.ObservedGeneration,
			LastTransitionTime: *cfg.LastTransitionTime,
		})
	}
	return out
}
