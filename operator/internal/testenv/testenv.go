// Copyright 2025 Redpanda Data, Inc.
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
	"math/rand/v2"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	goclientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/otelkube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
	"github.com/redpanda-data/redpanda-operator/pkg/vcluster"
)

type Env struct {
	t         *testing.T
	ctx       context.Context
	cancel    context.CancelFunc
	namespace *corev1.Namespace
	logger    logr.Logger
	scheme    *runtime.Scheme
	group     *errgroup.Group
	host      *k3d.Cluster
	config    *rest.Config
}

type Options struct {
	Name         string
	Agents       int
	SkipVCluster bool
	Scheme       *runtime.Scheme
	CRDs         []*apiextensionsv1.CustomResourceDefinition
	Logger       logr.Logger
	ImportImages []string
}

// New returns a configured [Env] that utilizes an [vcluster.Cluster] in a
// shared k3d cluster.
//
// Due to the shared nature, the k3d cluster will NOT be shutdown at the end of
// tests. The vCluster will be deleted unless -retain is specified.
func New(t *testing.T, options Options) *Env {
	if options.Agents == 0 {
		options.Agents = 3
	}

	if options.Name == "" {
		options.Name = k3d.SharedClusterName
	}

	if options.Scheme == nil {
		options.Scheme = goclientscheme.Scheme
	}

	if options.Logger.IsZero() {
		options.Logger = logr.Discard()
	}

	host, err := k3d.GetOrCreate(options.Name, k3d.WithAgents(options.Agents))
	require.NoError(t, err)

	for _, image := range options.ImportImages {
		require.NoError(t, host.ImportImage(image))
	}

	ctx, cancel := context.WithCancel(context.Background())

	var cluster *vcluster.Cluster
	config := host.RESTConfig()

	if !options.SkipVCluster {
		cluster, err = vcluster.New(ctx, host.RESTConfig())
		require.NoError(t, err)
		config = cluster.RESTConfig()
	}

	if len(options.CRDs) > 0 {
		crds, err := envtest.InstallCRDs(config, envtest.CRDInstallOptions{
			CRDs: dupCRDs(options.CRDs),
		})
		require.NoError(t, err)
		require.Equal(t, len(options.CRDs), len(crds))
	}

	c, err := client.New(config, client.Options{Scheme: options.Scheme})
	require.NoError(t, err)
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
		host:      host,
		config:    config,
	}

	if !options.SkipVCluster {
		t.Logf("Executing in namespace '%s' of vCluster '%s'", ns.Name, cluster.Name())
		t.Logf("Connect to vCluster using 'vcluster connect --namespace %s %s -- '", cluster.Name(), cluster.Name())
	} else {
		t.Logf("Executing in namespace '%s'", ns.Name)
	}

	t.Cleanup(func() {
		env.cancel()
		assert.NoError(env.t, env.group.Wait())

		if !testutil.Retain() {
			if !options.SkipVCluster {
				require.NoError(t, cluster.Delete())
			}

			// Clean up any clusters that aren't shared.
			if env.host.Name != k3d.SharedClusterName {
				require.NoError(t, env.host.Cleanup())
			}
		}
	})

	return env
}

func (e *Env) Client() client.Client {
	c, err := client.New(e.config, client.Options{
		Scheme: e.scheme,
	})
	require.NoError(e.t, err)

	return otelkube.NewClient(client.NewNamespacedClient(c, e.namespace.Name))
}

func (e *Env) Namespace() string {
	return e.namespace.Name
}

func (e *Env) SetupManager(serviceAccount string, fn func(ctrl.Manager) error) {
	// Bind the managers base config to a ServiceAccount via the "Impersonate"
	// feature. This ensures that any permissions/RBAC issues get caught by
	// theses tests as e.config has Admin permissions.
	config := rest.CopyConfig(e.config)
	if serviceAccount != "" {
		config.Impersonate.UserName = fmt.Sprintf("system:serviceaccount:%s:%s", e.Namespace(), serviceAccount)
	}

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

func RandString(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	name := ""
	for i := 0; i < length; i++ {
		//nolint:gosec // not meant to be a secure random string.
		name += string(alphabet[rand.IntN(len(alphabet))])
	}

	return name
}

func dupCRDs(crds []*apiextensionsv1.CustomResourceDefinition) []*apiextensionsv1.CustomResourceDefinition {
	cloned := []*apiextensionsv1.CustomResourceDefinition{}
	for _, crd := range crds {
		cloned = append(cloned, crd.DeepCopy())
	}
	return cloned
}
