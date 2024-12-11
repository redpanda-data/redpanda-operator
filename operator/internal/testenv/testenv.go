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
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/rand"
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

	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
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
	Name   string
	Agents int
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
	if options.Agents == 0 {
		options.Agents = 3
	}

	if options.Name == "" {
		options.Name = k3dClusterName
	}

	if options.Scheme == nil {
		options.Scheme = goclientscheme.Scheme
	}

	if options.Logger.IsZero() {
		options.Logger = logr.Discard()
	}

	cluster, err := k3d.GetOrCreate(options.Name, k3d.WithAgents(options.Agents))
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

func (e *Env) Namespace() string {
	return e.namespace.Name
}

func (e *Env) SetupManager(serviceAccount string, fn func(ctrl.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)

	// TODO: Webhooks likely aren't going to place nicely with this method of
	// testing. The Kube API server will have to dial out of the cluster to the
	// local machine which could prove to be difficult across all docker/docker
	// in docker environments.
	// See also https://k3d.io/v5.4.6/faq/faq/?h=host#how-to-access-services-like-a-database-running-on-my-docker-host-machine
	manager, err := ctrl.NewManager(config, ctrl.Options{
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
		if err := manager.Start(e.ctx); err != nil && e.ctx.Err() != nil {
			return err
		}
		return nil
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
	gvk, err := c.GroupVersionKindFor(e.namespace)
	if err != nil {
		panic(err)
	}

	apiVersion, kind := gvk.ToAPIVersionAndKind()

	// Bind all operations to this namespace. We'll delete it at the end of this test.
	c = client.NewNamespacedClient(c, e.namespace.Name)
	// For any non-namespaced resources, we'll attach an OwnerReference to our
	// Namespace to ensure they get cleaned up as well.
	c = newOwnedClient(c, metav1.OwnerReference{
		APIVersion:         apiVersion,
		Kind:               kind,
		UID:                e.namespace.UID,
		Name:               e.namespace.Name,
		BlockOwnerDeletion: ptr.To(true),
	})
	return c
}

func (e *Env) shutdown() {
	// Our shutdown logic is unfortunately complicated. The manager is using a
	// ServiceAccount and roles that will be GC'd by the Namespace deletion due
	// to the OwnedClient wrapper but the Namespace deletion could be blocked
	// by finalizers that would need to be removed by the manager itself.
	//
	// For now, we'll assume that tests have cleaned all resources associated
	// with the manager and shutdown the manager before cleaning out the
	// namespace.

	// Shutdown the manger.
	e.cancel()
	assert.NoError(e.t, e.group.Wait())

	// Clean up our namespace.
	if !testutil.Retain() {
		// Mint a new ctx for NS clean up as e.ctx has been canceled.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		c := e.client()

		assert.NoError(e.t, c.Delete(ctx, e.namespace, client.PropagationPolicy(metav1.DeletePropagationForeground)))

		// Poll until the namespace is fully deleted.
		assert.NoErrorf(e.t, wait.PollUntilContextTimeout(ctx, time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
			err = c.Get(ctx, client.ObjectKeyFromObject(e.namespace), e.namespace)
			return apierrors.IsNotFound(err), client.IgnoreNotFound(err)
		}), "stalled waiting for Namespace %q to finish deleting", e.namespace.Name)
	}
}

func RandString(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	name := ""
	for i := 0; i < length; i++ {
		//nolint:gosec // not meant to be a secure random string.
		name += string(alphabet[rand.Intn(len(alphabet))])
	}

	return name
}
