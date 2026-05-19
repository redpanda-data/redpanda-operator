// Copyright 2026 Redpanda Data, Inc.
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
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/version"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

const (
	finalizerKey     = "operator.redpanda.com/finalizer"
	managedByService = "redpanda-operator"
)

// console resources
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=consoles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=consoles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps;secrets;services;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete

type Controller struct {
	// Ctl is the kube.Ctl bound to the controller's local cluster. It is
	// used for reconciles whose req.ClusterName is the local cluster
	// (mcmanager.LocalCluster, i.e. the empty string) as well as for
	// setup-time reads (e.g. ServiceMonitor CRD discovery).
	Ctl    *kube.Ctl
	Config *kube.RESTConfig

	// Manager, when non-nil, enables per-cluster routing for reconciles
	// whose req.ClusterName names a provider cluster. SetupWithManager and
	// SetupWithMulticlusterManager both populate this so that
	// Reconcile-time reads and writes target the cluster where the Console
	// CR actually lives.
	Manager multicluster.Manager

	// rng is used to generate Console's JWT Signing keys, if they're not
	// explicitly specified. If nil, SetupWithManager will set it with a seeded
	// value.
	rng *rand.Rand

	// capabilitiesMu guards capabilitiesCached and capabilities. We cache
	// on first successful resolution since the K8s version doesn't change
	// during the operator's lifetime.
	capabilitiesMu     sync.Mutex
	capabilitiesCached bool
	capabilities       helmette.Capabilities
}

func (c *Controller) SetupWithManager(ctx context.Context, mgr multicluster.Manager, namespace string) error {
	c.Manager = mgr

	// If rng is not set for testing, create and seed a new one.
	if c.rng == nil {
		// TODO: Weak RNG is probably acceptable here but best to doublecheck
		c.rng = rand.New(rand.NewSource(time.Now().UnixMicro())) //nolint:gosec
	}

	builder := mcbuilder.ControllerManagedBy(mgr).
		For(&redpandav1alpha2.Console{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))

	// NB: As of writing, all console types are namespace scoped.
	for _, t := range console.Types() {
		// this will skip watch for ServiceMonitor if they are not installed in the cluster.
		// If it gets installed during the operator runtime, we will need to restart the operator to start watching for it.
		// While not ideal, given that we don't modify Console's ServiceMonitor at all, I think it's **fine**.
		if _, ok := t.(*monitoringv1.ServiceMonitor); ok {
			if c.skipWatchIfNotInstalled(ctx, &monitoringv1.ServiceMonitorList{}, "ServiceMonitors") {
				continue
			}
		}
		// Skip HTTPRoute watch if Gateway API CRDs are not installed.
		if _, ok := t.(*gatewayv1.HTTPRoute); ok {
			if c.skipWatchIfNotInstalled(ctx, &gatewayv1.HTTPRouteList{}, "HTTPRoutes") {
				continue
			}
		}
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

	return builder.Complete(controller.FilterNamespaceReconciler(namespace, c))
}

// SetupWithMulticlusterManager registers the Console controller against a
// multicluster manager (e.g. the raft-based runtime manager wired up by the
// `multicluster` subcommand). It is a superset of [Controller.SetupWithManager]
// that additionally watches StretchCluster CRs for re-enqueue, which is only
// safe in the multicluster operator mode where the StretchCluster CRD is
// guaranteed to be installed alongside the Console CRD.
//
// Mirrors the pattern established by NodePool's SetupWithMultiClusterManager.
func (c *Controller) SetupWithMulticlusterManager(ctx context.Context, mgr multicluster.Manager) error {
	c.Manager = mgr

	// If rng is not set for testing, create and seed a new one.
	if c.rng == nil {
		c.rng = rand.New(rand.NewSource(time.Now().UnixMicro())) //nolint:gosec
	}

	builder := mcbuilder.ControllerManagedBy(mgr).
		WithOptions(ctrlcontroller.TypedOptions[mcreconcile.Request]{
			// NB: This mirrors the workaround in
			// redpanda.SetupMulticlusterController. The multicluster runtime
			// doesn't propagate name-uniqueness validation cleanly when
			// multiple managers register the same controller in a single
			// process (e.g. integration tests spinning up N peer managers),
			// so we opt out. Consider an upstream fix.
			SkipNameValidation: ptr.To(true),
		}).
		For(&redpandav1alpha2.Console{}, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))

	for _, t := range console.Types() {
		if _, ok := t.(*monitoringv1.ServiceMonitor); ok {
			if c.skipWatchIfNotInstalled(ctx, &monitoringv1.ServiceMonitorList{}, "ServiceMonitors") {
				continue
			}
		}
		if _, ok := t.(*gatewayv1.HTTPRoute); ok {
			if c.skipWatchIfNotInstalled(ctx, &gatewayv1.HTTPRouteList{}, "HTTPRoutes") {
				continue
			}
		}
		builder = builder.Owns(t, mcbuilder.WithEngageWithLocalCluster(true), mcbuilder.WithEngageWithProviderClusters(true))
	}

	// Re-enqueue Console CRs that reference a StretchCluster. Redpanda CRs
	// are not deployed in multicluster mode, so no Redpanda watch is needed.
	for _, clusterName := range mgr.GetClusterNames() {
		stretchHandler, err := controller.RegisterStretchClusterSourceIndex(ctx, mgr, "console_stretch", clusterName, &redpandav1alpha2.Console{}, &redpandav1alpha2.ConsoleList{})
		if err != nil {
			return err
		}
		builder.Watches(&redpandav1alpha2.StretchCluster{}, stretchHandler, controller.WatchOptions(clusterName)...)
	}

	return builder.Complete(c)
}

func (c *Controller) Reconcile(ctx context.Context, req mcreconcile.Request) (ctrl.Result, error) {
	ctl, err := c.ctlFor(ctx, req.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	cr, err := kube.Get[redpandav1alpha2.Console](ctx, ctl, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	syncer, err := c.syncerFor(ctl, cr)
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
		if err := ctl.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.jwtSecretName(cr),
				Namespace: cr.Namespace,
			},
		}); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		// NB: Apply can't be used to remove finalizers.
		if err := ctl.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	log.Info(ctx, "reconciling Console", "key", kube.AsKey(cr))

	// Add the finalizer, if not present.
	if controllerutil.AddFinalizer(cr, finalizerKey) {
		if err := ctl.Apply(ctx, cr, client.ForceOwnership); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := c.maybeSetJWTToken(ctx, ctl, cr); err != nil {
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

	if err := ctl.ApplyStatus(ctx, cr, client.ForceOwnership); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ctlFor returns a [kube.Ctl] scoped to the given clusterName. For the local
// cluster (empty string == [mcmanager.LocalCluster]) — and for any case where
// the controller has no Manager wired in — it returns the externally-supplied
// c.Ctl. Otherwise it builds a fresh [kube.Ctl] against the peer cluster's
// REST config / scheme / cache for this reconcile pass; mirrors the
// per-reconcile [cluster.Cluster.GetClient] pattern used by other
// multicluster reconcilers (see NodePoolReconciler).
func (c *Controller) ctlFor(ctx context.Context, clusterName string) (*kube.Ctl, error) {
	if clusterName == mcmanager.LocalCluster || c.Manager == nil {
		return c.Ctl, nil
	}

	cluster, err := c.Manager.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("looking up cluster %q: %w", clusterName, err)
	}

	ctl, err := kube.FromRESTConfig(cluster.GetConfig(), kube.Options{
		Options: client.Options{
			Scheme: cluster.GetScheme(),
			Cache:  &client.CacheOptions{Reader: cluster.GetCache()},
		},
		FieldManager: string(lifecycle.DefaultFieldOwner),
	})
	if err != nil {
		return nil, fmt.Errorf("building kube.Ctl for cluster %q: %w", clusterName, err)
	}
	return ctl, nil
}

func (c *Controller) syncerFor(ctl *kube.Ctl, cr *redpandav1alpha2.Console) (*kube.Syncer, error) {
	gvk, err := kube.GVKFor(ctl.Scheme(), cr)
	if err != nil {
		return nil, err
	}

	return &kube.Syncer{
		Ctl:             ctl,
		Namespace:       cr.Namespace,
		Renderer:        c.rendererFor(ctl, cr),
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

func (c *Controller) rendererFor(ctl *kube.Ctl, cr *redpandav1alpha2.Console) *render {
	caps := c.resolveCapabilities()

	metrics := console.MetricsState{
		ViaOperator:       true,
		KubernetesVersion: caps.KubeVersion.Version,
		ChartVersion:      version.Version,
	}

	// Detect cloud environment from Kubernetes version string.
	if strings.Contains(caps.KubeVersion.Version, "-gke") {
		metrics.CloudEnvironment = "GCP"
	} else if strings.Contains(caps.KubeVersion.Version, "-eks") {
		metrics.CloudEnvironment = "AWS"
	}

	return &render{
		ctl:     ctl,
		console: cr,
		labels:  c.ownershipLabelsFor(cr),
		metrics: metrics,
	}
}

func (c *Controller) resolveCapabilities() helmette.Capabilities {
	c.capabilitiesMu.Lock()
	defer c.capabilitiesMu.Unlock()

	if c.capabilitiesCached {
		return c.capabilities
	}

	caps, err := helmette.NewCapabilities(c.Config)
	if err != nil {
		return helmette.Capabilities{}
	}

	c.capabilities = caps
	c.capabilitiesCached = true
	return caps
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
func (c *Controller) maybeSetJWTToken(ctx context.Context, ctl *kube.Ctl, cr *redpandav1alpha2.Console) error {
	explicitJWTKey := cr.Spec.Secret.Authentication != nil && cr.Spec.Secret.Authentication.JWTSigningKey != nil
	if explicitJWTKey {
		return nil
	}

	name := c.jwtSecretName(cr)

	secret, err := kube.Get[corev1.Secret](ctx, ctl, kube.ObjectKey{Namespace: cr.Namespace, Name: name})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if secret == nil {
		gvk, err := kube.GVKFor(ctl.Scheme(), cr)
		if err != nil {
			return err
		}

		// NB: Create and Immutable are used here to ensure that key generation
		// is idempotent. Immutable prevents accidental updates or changes and
		// Create prevents race conditions from causing a re-mint.
		secret, err = kube.Create(ctx, ctl, corev1.Secret{
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

func (c *Controller) skipWatchIfNotInstalled(ctx context.Context, list client.ObjectList, name string) (skip bool) {
	err := c.Ctl.List(ctx, "default", list)
	if errors.Is(err, &meta.NoKindMatchError{}) {
		return true
	} else if err != nil {
		log.Error(ctx, err, "could not list "+name)
		return true
	}
	return false
}

// render implements [kube.Renderer].
type render struct {
	ctl     *kube.Ctl
	labels  map[string]string
	console *redpandav1alpha2.Console
	metrics console.MetricsState
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

	state, err := console.NewRenderState(r.console.Namespace, r.console.Name, r.labels, clusterValues)
	if err != nil {
		return nil, err
	}

	// Populate metrics telemetry state. Cluster ID is resolved per-reconcile
	// as the controller may not have access at startup.
	metrics := r.metrics
	if metrics.ClusterID == "" {
		var ns corev1.Namespace
		if err := r.ctl.Get(ctx, kube.ObjectKey{Name: "kube-system"}, &ns); err == nil {
			metrics.ClusterID = string(ns.UID)
		}
	}
	state.Metrics = metrics

	return state, nil
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
			Namespace: ref.GetNamespace(r.console.Namespace),
		}

		if ref.IsStretchCluster() {
			var sc redpandav1alpha2.StretchCluster
			if err := r.ctl.Get(ctx, key, &sc); err != nil {
				return console.PartialRenderValues{}, err
			}

			// TLS / Listeners / ClusterDomain live on each pool's spec (to
			// support heterogeneous pools), so we need a representative
			// RedpandaBrokerPool to derive the Console connection config.
			// Pick the first pool in this cluster that references the
			// StretchCluster; if none exists yet, return an empty fragment
			// — the Console will be re-reconciled when pools appear (see
			// SetupWithMulticlusterManager's StretchCluster watch).
			pool, err := r.findRepresentativePool(ctx, &sc)
			if err != nil {
				return console.PartialRenderValues{}, err
			}
			if pool == nil {
				return console.PartialRenderValues{}, nil
			}

			cfg := conversion.ConvertStretchClusterToStaticConfig(&sc, pool)
			return console.StaticConfigurationSourceToPartialRenderValues(cfg), nil
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

// findRepresentativePool returns the first RedpandaBrokerPool in the
// StretchCluster's namespace that references sc. Pools live in the same
// K8s cluster as the Console (the local cluster the renderer's r.ctl is
// scoped to). Returns (nil, nil) if no matching pool exists yet — Console
// will be re-reconciled via the StretchCluster watch as pools come and go.
func (r *render) findRepresentativePool(ctx context.Context, sc *redpandav1alpha2.StretchCluster) (*redpandav1alpha2.RedpandaBrokerPool, error) {
	var pools redpandav1alpha2.RedpandaBrokerPoolList
	if err := r.ctl.List(ctx, sc.Namespace, &pools); err != nil {
		return nil, err
	}
	for i := range pools.Items {
		pool := &pools.Items[i]
		ref := pool.Spec.ClusterRef
		if ref.IsStretchCluster() && ref.Name == sc.Name {
			return pool, nil
		}
	}
	return nil, fmt.Errorf("no RedpandaBrokerPool found in this k8s cluster for stretch cluster %s. Please create one before creating Console", sc.Name)
}
