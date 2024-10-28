// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/redpanda-data/helm-charts/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const k3dClusterName = "testenv"

func init() {
	ctrl.SetLogger(logr.Discard()) // Silence the dramatic logger messages.
}

type Env struct {
	t         *testing.T
	logger    logr.Logger
	scheme    *runtime.Scheme
	namespace *corev1.Namespace
	config    *rest.Config
	ctx       context.Context
	cancel    context.CancelFunc
	group     *errgroup.Group
}

type Options struct {
	Scheme *runtime.Scheme
	CRDs   []*apiextensionsv1.CustomResourceDefinition
	Logger logr.Logger
}

// New returns a configured [Env] that utilizes an isolated namespace in a
// shared k3d cluster.
//
// Isolation is implemented at the [client.Client] level by limiting namespaced
// requests to the provisioned namespace and attaching an owner reference to
// the provisioned namespace to all non-namespaced requests.
//
// Due to the shared nature, the k3d cluster will NOT be shutdown at the end of
// tests.
func New(t *testing.T, options Options) *Env {
	if options.Scheme == nil {
		options.Scheme = goclientscheme.Scheme
	}

	if options.Logger.IsZero() {
		options.Logger = logr.Discard()
	}

	// TODO maybe allow customizing name?
	cluster, err := k3d.GetOrCreate(k3dClusterName)
	require.NoError(t, err)

	if len(options.CRDs) > 0 {
		// CRDs are cluster scoped, so there's not a great way for us to safely
		// allow multi-tenancy. Instead, we're just crossing our fingers and
		// hoping for the best.
		crds, err := envtest.InstallCRDs(cluster.RESTConfig(), envtest.CRDInstallOptions{
			CRDs: options.CRDs,
		})
		require.NoError(t, err)
		require.Equal(t, len(options.CRDs), len(crds))
	}

	c, err := client.New(cluster.RESTConfig(), client.Options{Scheme: options.Scheme})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	g, ctx := errgroup.WithContext(ctx)

	// Create a unique Namespace to perform tests within.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		GenerateName: "testenv-",
	}}

	require.NoError(t, c.Create(ctx, ns))

	env := &Env{
		t:         t,
		scheme:    options.Scheme,
		logger:    options.Logger,
		namespace: ns,
		group:     g,
		ctx:       ctx,
		cancel:    cancel,
		config:    cluster.RESTConfig(),
	}

	t.Logf("Executing in namespace '%s'", ns.Name)

	t.Cleanup(env.shutdown)

	return env
}

func (e *Env) Client() client.Client {
	return e.wrapClient(e.client())
}

func (e *Env) SetupManager(fn func(ctrl.Manager) error) {
	// TODO: Webhooks likely aren't going to place nicely with this method of
	// testing. The Kube API server will have to dial out of the cluster to the
	// local machine which could prove to be difficult across all docker/docker
	// in docker environments.
	// See also https://k3d.io/v5.4.6/faq/faq/?h=host#how-to-access-services-like-a-database-running-on-my-docker-host-machine
	manager, err := ctrl.NewManager(e.config, ctrl.Options{
		Cache: cache.Options{
			// Limit this manager to only interacting with objects within our
			// namespace.
			DefaultNamespaces: map[string]cache.Config{
				e.namespace.Name: {},
			},
		},
		Metrics: server.Options{BindAddress: "0"}, // Disable metrics server to avoid port conflicts.
		Scheme:  e.scheme,
		Logger:  e.logger,
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			c, err := client.New(config, options)
			if err != nil {
				return nil, err
			}
			// Wrap any clients created by the manager to ensure namespace
			// isolation or GC tracking.
			return e.wrapClient(c), nil
		},
		BaseContext: func() context.Context {
			return e.ctx
		},
	})
	require.NoError(e.t, err)

	require.NoError(e.t, fn(manager))

	e.group.Go(func() error {
		return manager.Start(e.ctx)
	})

	// No Without leader election enabled, this is just a wait for the manager
	// to start up.
	<-manager.Elected()
}

func (e *Env) client() client.Client {
	c, err := client.New(e.config, client.Options{
		Scheme: e.scheme,
	})
	require.NoError(e.t, err)
	return c
}

func (e *Env) wrapClient(c client.Client) client.Client {
	// Bind all operations to this namespace. We'll delete it at the end of this test.
	c = client.NewNamespacedClient(c, e.namespace.Name)
	// For any non-namespaced resources, we'll attach an OwnerReference to our
	// Namespace to ensure they get cleaned up as well.
	c = newOwnedClient(c, metav1.OwnerReference{
		APIVersion:         e.namespace.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:               e.namespace.GetObjectKind().GroupVersionKind().Kind,
		UID:                e.namespace.UID,
		Name:               e.namespace.Name,
		BlockOwnerDeletion: ptr.To(true),
	})
	return c
}

func (e *Env) shutdown() {
	if !testutil.Retain() {
		// NB: Namespace deletion MUST happen before calling e.cancel.
		// Otherwise we risk stopping controllers that would remove finalizers
		// from various resources in the cluster which would hang the namespace
		// deletion forever.
		c := e.client()

		assert.NoError(e.t, c.Delete(e.ctx, e.namespace, client.PropagationPolicy(metav1.DeletePropagationForeground)))

		// Poll until the namespace is fully deleted.
		assert.NoError(e.t, wait.PollUntilContextTimeout(e.ctx, time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
			err = c.Get(ctx, client.ObjectKeyFromObject(e.namespace), e.namespace)
			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		}))
	}

	e.cancel()
	assert.NoError(e.t, e.group.Wait())
}
