// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package connect implements the controller for the Connect CRD.
package connect

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/license"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	FinalizerKey = "connect.redpanda.com/finalizer"

	// Default resource requests for Connect pods.
	defaultMemoryRequest = "256Mi"
	defaultCPURequest    = "100m"

	// Default license key in Secret data.
	defaultLicenseKey = "license"
)

// Controller reconciles Connect resources.
type Controller struct {
	client.Client
}

// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=connects,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=connects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=connects/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (c *Controller) SetupWithManager(ctx context.Context, mgr ctrl.Manager, namespace string) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&redpandav1alpha2.Connect{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{})

	return builder.Complete(c)
}

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("connect")

	var connect redpandav1alpha2.Connect
	if err := c.Get(ctx, req.NamespacedName, &connect); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !connect.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&connect, FinalizerKey) {
			controllerutil.RemoveFinalizer(&connect, FinalizerKey)
			if err := c.Update(ctx, &connect); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if missing.
	if !controllerutil.ContainsFinalizer(&connect, FinalizerKey) {
		controllerutil.AddFinalizer(&connect, FinalizerKey)
		if err := c.Update(ctx, &connect); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate license before proceeding.
	if err := c.validateLicense(ctx, &connect); err != nil {
		logger.Error(err, "license validation failed")
		c.setCondition(&connect, metav1.ConditionFalse, "LicenseInvalid", err.Error())
		if updateErr := c.Status().Update(ctx, &connect); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		// Requeue to retry license check periodically.
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}

	// Reconcile ConfigMap.
	if err := c.reconcileConfigMap(ctx, &connect); err != nil {
		logger.Error(err, "failed to reconcile ConfigMap")
		c.setCondition(&connect, metav1.ConditionFalse, redpandav1alpha2.FailedReason, fmt.Sprintf("ConfigMap reconciliation failed: %v", err))
		_ = c.Status().Update(ctx, &connect)
		return ctrl.Result{}, err
	}

	// Reconcile Deployment.
	if err := c.reconcileDeployment(ctx, &connect); err != nil {
		logger.Error(err, "failed to reconcile Deployment")
		c.setCondition(&connect, metav1.ConditionFalse, redpandav1alpha2.FailedReason, fmt.Sprintf("Deployment reconciliation failed: %v", err))
		_ = c.Status().Update(ctx, &connect)
		return ctrl.Result{}, err
	}

	// Update status from Deployment.
	if err := c.updateStatus(ctx, &connect); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (c *Controller) validateLicense(ctx context.Context, connect *redpandav1alpha2.Connect) error {
	ref := connect.Spec.LicenseSecretRef
	secretName := ref.Name
	key := ref.Key
	if key == "" {
		key = defaultLicenseKey
	}

	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: connect.Namespace,
	}, &secret); err != nil {
		return errors.Wrap(err, "failed to get license secret")
	}

	data, ok := secret.Data[key]
	if !ok {
		return errors.Newf("key %q not found in Secret %s/%s", key, connect.Namespace, secretName)
	}

	l, err := license.ParseLicense(data)
	if err != nil {
		return errors.Wrap(err, "failed to parse license")
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

func (c *Controller) reconcileConfigMap(ctx context.Context, connect *redpandav1alpha2.Connect) error {
	name := connect.Name
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: connect.Namespace}}

	_, err := controllerutil.CreateOrPatch(ctx, c.Client, cm, func() error {
		cm.Data = map[string]string{
			"connect.yaml": connect.Spec.ConfigYAML,
		}
		for filename, content := range connect.Spec.ConfigFiles {
			if filename == "connect.yaml" {
				return errors.New("configFiles cannot contain a key named \"connect.yaml\"; use configYaml instead")
			}
			cm.Data[filename] = content
		}
		return controllerutil.SetControllerReference(connect, cm, c.Scheme())
	})
	return err
}

func (c *Controller) reconcileDeployment(ctx context.Context, connect *redpandav1alpha2.Connect) error {
	name := connect.Name
	replicas := connect.GetReplicas()
	image := connect.GetImage()

	labels := map[string]string{
		"app.kubernetes.io/name":       "redpanda-connect",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/component":  "connect-pipeline",
	}

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(defaultMemoryRequest),
			corev1.ResourceCPU:    resource.MustParse(defaultCPURequest),
		},
	}
	if connect.Spec.Resources != nil {
		resources = *connect.Spec.Resources
	}

	dp := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: connect.Namespace}}

	_, err := controllerutil.CreateOrPatch(ctx, c.Client, dp, func() error {
		dp.Spec.Replicas = ptr.To(replicas)
		dp.Spec.Strategy = appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType,
		}

		// Selector is immutable — only set on creation.
		if dp.CreationTimestamp.IsZero() {
			dp.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: labels,
			}
		}

		dp.Spec.Template = corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "connect",
						Image:   image,
						Command: []string{"redpanda-connect", "run", "/config/connect.yaml"},
						Ports: []corev1.ContainerPort{
							{Name: "http", ContainerPort: 4195, Protocol: corev1.ProtocolTCP},
						},
						Env:       connect.Spec.Env,
						Resources: resources,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "config",
								MountPath: "/config",
								ReadOnly:  true,
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: name,
								},
							},
						},
					},
				},
			},
		}

		return controllerutil.SetControllerReference(connect, dp, c.Scheme())
	})
	return err
}

func (c *Controller) updateStatus(ctx context.Context, connect *redpandav1alpha2.Connect) error {
	name := connect.Name

	var dp appsv1.Deployment
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: connect.Namespace}, &dp); err != nil {
		if apierrors.IsNotFound(err) {
			connect.Status.Phase = "Pending"
			connect.Status.Replicas = 0
			connect.Status.ReadyReplicas = 0
			c.setCondition(connect, metav1.ConditionFalse, redpandav1alpha2.ProgressingReason, "Deployment not yet created")
		} else {
			return err
		}
	} else {
		connect.Status.Replicas = dp.Status.Replicas
		connect.Status.ReadyReplicas = dp.Status.ReadyReplicas

		switch {
		case connect.Spec.Paused:
			connect.Status.Phase = "Stopped"
			c.setCondition(connect, metav1.ConditionTrue, redpandav1alpha2.SucceededReason, "Pipeline is paused")
		case dp.Status.ReadyReplicas == dp.Status.Replicas && dp.Status.Replicas > 0:
			connect.Status.Phase = "Running"
			c.setCondition(connect, metav1.ConditionTrue, redpandav1alpha2.SucceededReason, "Pipeline is running")
		case dp.Status.ReadyReplicas < dp.Status.Replicas:
			connect.Status.Phase = "Provisioning"
			c.setCondition(connect, metav1.ConditionFalse, redpandav1alpha2.ProgressingReason, "Pipeline is starting up")
		default:
			connect.Status.Phase = "Unknown"
			c.setCondition(connect, metav1.ConditionFalse, redpandav1alpha2.ProgressingReason, "Pipeline status unknown")
		}
	}

	connect.Status.ObservedGeneration = connect.Generation
	return c.Status().Update(ctx, connect)
}

func (c *Controller) setCondition(connect *redpandav1alpha2.Connect, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               redpandav1alpha2.ReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: connect.Generation,
		LastTransitionTime: metav1.Now(),
	}

	for i := range connect.Status.Conditions {
		if connect.Status.Conditions[i].Type == redpandav1alpha2.ReadyCondition {
			if connect.Status.Conditions[i].Status == status &&
				connect.Status.Conditions[i].Reason == reason {
				return
			}
			connect.Status.Conditions[i] = condition
			return
		}
	}

	connect.Status.Conditions = append(connect.Status.Conditions, condition)
}
