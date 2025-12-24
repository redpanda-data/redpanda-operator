// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
)

const (
	finalizerKey     = "operator.redpanda.com/finalizer"
	managedByService = "redpanda-operator"
)

// console resources
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=consoles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=consoles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

type Controller struct {
	Ctl *kube.Ctl

	// rng is used to generate Console's JWT Signing keys, if they're not
	// explicitly specified. If nil, SetupWithManager will set it with a seeded
	// value.
	rng *rand.Rand
}

func (c *Controller) SetupWithManager(ctx context.Context, mgr multicluster.Manager) error {
	// If rng is not set for testing, create and seed a new one.
	if c.rng == nil {
		// TODO: Weak RNG is probably acceptable here but best to doublecheck
		c.rng = rand.New(rand.NewSource(time.Now().UnixMicro())) //nolint:gosec
	}

	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.Console{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))

	// NB: As of writing, all console types are namespace scoped.
	for _, t := range console.Types() {
		builder = builder.Owns(t, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))
	}

	for _, clusterName := range mgr.GetClusterNames() {
		eventHandler, err := controller.RegisterClusterSourceIndex(ctx, mgr, "console", clusterName, &redpandav1alpha2.Console{}, &redpandav1alpha2.ConsoleList{})
		if err != nil {
			return err
		}

		// Configure a watch on redpandas using controller-runtime's indexing.
		// If a redpanda is updated, any console's referring to it will be
		// re-reconciled.
		builder.Watches(&redpandav1alpha2.Redpanda{}, eventHandler, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(c)
}

func (c *Controller) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	cr, err := kube.Get[redpandav1alpha2.Console](ctx, c.Ctl, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	syncer, err := c.syncerFor(cr)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If we're being deleted, clean up any left over objects and remove the
	// finalizer. As of writing, there are no Cluster scope resources deployed
	// so we don't NEED to use a finalizer. It's here to prevent accidents if
	// we need cluster scoped resources one day.
	if !cr.DeletionTimestamp.IsZero() {
		log.Info(ctx, "GC'ing Console", "key", kube.AsKey(cr))

		if controllerutil.RemoveFinalizer(cr, finalizerKey) {
			if _, err := syncer.DeleteAll(ctx); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Clean up the JWT secret, if it exists.
		if err := c.Ctl.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.jwtSecretName(cr),
				Namespace: cr.Namespace,
			},
		}); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		// NB: Apply can't be used to remove finalizers.
		if err := c.Ctl.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	log.Info(ctx, "reconciling Console", "key", kube.AsKey(cr))

	// Add the finalizer, if not present.
	if controllerutil.AddFinalizer(cr, finalizerKey) {
		if err := c.Ctl.Apply(ctx, cr, client.ForceOwnership); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := c.maybeSetJWTToken(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	objs, err := syncer.Sync(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, obj := range objs {
		switch obj := obj.(type) {
		case *appsv1.Deployment:
			// Only advance ObservedGeneration if we've successfully applied a
			// Deployment.
			cr.Status.ObservedGeneration = cr.Generation

			cr.Status.AvailableReplicas = obj.Status.AvailableReplicas
			cr.Status.ReadyReplicas = obj.Status.ReadyReplicas
			cr.Status.Replicas = obj.Status.Replicas
			cr.Status.UnavailableReplicas = obj.Status.UnavailableReplicas
			cr.Status.UpdatedReplicas = obj.Status.UpdatedReplicas
		}
	}

	if err := c.Ctl.ApplyStatus(ctx, cr, client.ForceOwnership); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (c *Controller) syncerFor(cr *redpandav1alpha2.Console) (*kube.Syncer, error) {
	gvk, err := kube.GVKFor(c.Ctl.Scheme(), cr)
	if err != nil {
		return nil, err
	}

	return &kube.Syncer{
		Ctl:             c.Ctl,
		Namespace:       cr.Namespace,
		Renderer:        c.rendererFor(cr),
		Owner:           *metav1.NewControllerRef(cr, gvk),
		OwnershipLabels: c.ownershipLabelsFor(cr),
	}, nil
}

func (c *Controller) ownershipLabelsFor(cr *redpandav1alpha2.Console) map[string]string {
	return map[string]string{
		// These labels are technically applied by the chart but we re-apply them
		// here so we can use them to manage resource ownership as well.
		"app.kubernetes.io/name":       console.ChartName,
		"app.kubernetes.io/managed-by": managedByService,
		"app.kubernetes.io/instance":   cr.Name,
	}
}

func (c *Controller) rendererFor(console *redpandav1alpha2.Console) *render {
	return &render{
		ctl:     c.Ctl,
		console: console,
		labels:  c.ownershipLabelsFor(console),
	}
}

func (c *Controller) randKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		// Printable ASCII characters are in the range 31-127.
		key[i] = byte(c.rng.Intn(127-31) + 31)
	}
	return key
}

func (c *Controller) jwtSecretName(cr *redpandav1alpha2.Console) string {
	return fmt.Sprintf("%s-jwt-secret", cr.Name)
}

// maybeSetJWTToken idempotently sets the [Console]'s JWTSigningKey, if one is
// not explicitly provided.
//
// This generated key is stored in an immutable secret that is managed outside
// of the Syncer to prevent re-minting.
func (c *Controller) maybeSetJWTToken(ctx context.Context, cr *redpandav1alpha2.Console) error {
	explicitJWTKey := cr.Spec.Secret.Authentication != nil && cr.Spec.Secret.Authentication.JWTSigningKey != nil
	if explicitJWTKey {
		return nil
	}

	name := c.jwtSecretName(cr)

	secret, err := kube.Get[corev1.Secret](ctx, c.Ctl, kube.ObjectKey{Namespace: cr.Namespace, Name: name})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if secret == nil {
		gvk, err := kube.GVKFor(c.Ctl.Scheme(), cr)
		if err != nil {
			return err
		}

		// NB: Create and Immutable are used here to ensure that key generation
		// is idempotent. Immutable prevents accidental updates or changes and
		// Create prevents race conditions from causing a re-mint.
		secret, err = kube.Create(ctx, c.Ctl, corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: cr.Namespace,
				// NB: ownership labels are explicitly NOT set here. This
				// object is out of scope of the syncer.
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(cr, gvk),
				},
			},
			Immutable: ptr.To(true),
			Data: map[string][]byte{
				"key": c.randKey(),
			},
		})
		if err != nil {
			return err
		}
	}

	if cr.Spec.Secret.Authentication == nil {
		cr.Spec.Secret.Authentication = &redpandav1alpha2.AuthenticationSecrets{}
	}

	cr.Spec.Secret.Authentication.JWTSigningKey = ptr.To(string(secret.Data["key"]))

	return nil
}

// render implements [kube.Renderer].
type render struct {
	ctl     *kube.Ctl
	labels  map[string]string
	console *redpandav1alpha2.Console
}

func (r *render) Types() []kube.Object {
	return console.Types()
}

func (r *render) Render(ctx context.Context) ([]kube.Object, error) {
	state, err := r.state(ctx)
	if err != nil {
		return nil, err
	}

	objs := console.Render(state)

	return functional.Filter(objs, func(obj kube.Object) bool {
		return !reflect.ValueOf(obj).IsNil()
	}), nil
}

func (r *render) state(ctx context.Context) (*console.RenderState, error) {
	clusterValues, err := r.clusterFragment(ctx)
	if err != nil {
		return nil, err
	}

	// Convert the Console CR into chart values.
	userValues, err := redpandav1alpha2.ConvertConsoleToConsolePartialRenderValues(&r.console.Spec.ConsoleValues)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Merge the user values into the generated cluster configuration.
	if err := mergo.Merge(&clusterValues, userValues, mergo.WithAppendSlice); err != nil {
		return nil, errors.WithStack(err)
	}

	return console.NewRenderState(r.console.Namespace, r.console.Name, r.labels, clusterValues)
}

// clusterFragment returns a [console.PartialRenderValues] containing details
// on how to connect to the cluster specified in ClusterSource.
func (r *render) clusterFragment(ctx context.Context) (console.PartialRenderValues, error) {
	if r.console.Spec.ClusterSource == nil {
		return console.PartialRenderValues{}, nil
	}

	if ref := r.console.Spec.ClusterSource.ClusterRef; ref != nil {
		key := kube.ObjectKey{
			Name:      ref.Name,
			Namespace: r.console.Namespace,
		}

		// TODO: Add support for vectorized clusters?
		var rp redpandav1alpha2.Redpanda
		if err := r.ctl.Get(ctx, key, &rp); err != nil {
			return console.PartialRenderValues{}, err
		}

		state, err := conversion.ConvertV2ToRenderState(nil, &conversion.V2Defaulters{
			RedpandaImage: func(ri *redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage { return ri },
			SidecarImage:  func(ri *redpandav1alpha2.RedpandaImage) *redpandav1alpha2.RedpandaImage { return ri },
		}, &rp, nil)
		if err != nil {
			return console.PartialRenderValues{}, err
		}

		cfg := state.AsStaticConfigSource()
		return console.StaticConfigurationSourceToPartialRenderValues(&cfg), nil
	}

	if cfg := r.console.Spec.ClusterSource.StaticConfiguration; cfg != nil {
		irCfg := redpandav1alpha2.ConvertStaticConfigToIR(r.console.Namespace, cfg)

		return console.StaticConfigurationSourceToPartialRenderValues(irCfg), nil
	}

	return console.PartialRenderValues{}, nil
}
