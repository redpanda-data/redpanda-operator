// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package vcluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

const (
	// vClusterChartVersion is the pinned version of the vCluster helm chart. It's
	// pinned to avoid sudden failures if there are backwards incompatible changes
	// added.
	vClusterChartVersion    = "v0.28.0"
	certManagerChartversion = "v1.8.0"
)

type Cluster struct {
	config     *kube.RESTConfig
	hostConfig *kube.RESTConfig
	helm       *helm.Client
	release    helm.Release
	namespace  *corev1.Namespace
	scheme     *runtime.Scheme
}

func (c *Cluster) AsRESTClientGetter() genericclioptions.RESTClientGetter {
	return &vclusterRESTClientGetter{cluster: c}
}

func ForTestInShared(t *testing.T) *Cluster {
	cluster, err := NewInShared(t.Context())
	require.NoError(t, err)

	testutil.MaybeCleanup(t, func() {
		require.NoError(t, cluster.Delete())
	})

	return cluster
}

func ForTest(t *testing.T, host *k3d.Cluster) *Cluster {
	cluster, err := New(t.Context(), host.RESTConfig())
	require.NoError(t, err)

	testutil.MaybeCleanup(t, func() {
		require.NoError(t, cluster.Delete())
	})

	return cluster
}

func NewInShared(ctx context.Context) (*Cluster, error) {
	host, err := k3d.GetShared()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	cl, err := New(ctx, host.RESTConfig())
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return cl, nil
}

func New(ctx context.Context, config *kube.RESTConfig) (*Cluster, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, 3*time.Minute, errors.New("vCluster creation timed out"))
	defer cancel()

	c, err := client.New(config, client.Options{})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vcluster-",
		},
	}
	if err := c.Create(ctx, namespace); err != nil {
		return nil, errors.WithStack(err)
	}

	hc, err := helm.New(helm.Options{
		KubeConfig: config,
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if err := hc.RepoAdd(ctx, "loft", "https://charts.loft.sh"); err != nil {
		return nil, errors.WithStack(err)
	}

	rel, err := hc.Install(ctx, "loft/vcluster", helm.InstallOptions{
		Name:      namespace.Name,
		Namespace: namespace.Name,
		Version:   vClusterChartVersion,
		Values: map[string]any{
			"sync": map[string]any{
				"fromHost": map[string]any{
					"nodes": map[string]any{
						"enabled": true,
						"selector": map[string]any{
							"all": true,
						},
					},
				},
			},
			// TODO we can select other k8s distros. By default full k8s is
			// run. Swapping to k3s might result in some speed ups but initial
			// tests indicated that something wasn't working.
			"experimental": map[string]any{
				"deploy": map[string]any{
					"vcluster": map[string]any{
						// Being able to vendor the chart would save us a bit of time and flakiness.
						// There's support for a "bundle" containing a targz.
						"helm": []map[string]any{
							{
								"chart": map[string]any{
									"name":    "cert-manager",
									"repo":    "https://charts.jetstack.io",
									"version": certManagerChartversion,
								},
								"values": "\ninstallCRDs: true\n",
								"release": map[string]any{
									"name":      "cert-manager",
									"namespace": "cert-manager",
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var kubeConfig corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: rel.Namespace, Name: "vc-" + rel.Name}, &kubeConfig); err != nil {
		return nil, errors.WithStack(err)
	}

	apiConfig, err := clientcmd.Load(kubeConfig.Data["config"])
	if err != nil {
		return nil, err
	}

	cfg, err := kube.ConfigToRest(*apiConfig)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// To access the vCluster's API server, we dial into the api-server Pod on the host.
	dialer := kube.NewPodDialer(config)

	cfg.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		// It's fairly safe to assume that all connections are meant for the
		// API server as this function is only accessible via the vCluster's
		// rest config.
		idx := strings.LastIndex(address, ":")
		return dialer.DialContext(ctx, network, fmt.Sprintf("%s-0.%s:%s", rel.Name, rel.Namespace, address[idx+1:]))
	}

	return &Cluster{
		config:     cfg,
		helm:       hc,
		release:    rel,
		hostConfig: config,
		namespace:  namespace,
	}, nil
}

func (c *Cluster) Name() string {
	return c.namespace.Name
}

// Client is a convenience method to construct a [client.Client] from
// [Cluster.RestConfig].
func (c *Cluster) Client(options client.Options) (client.Client, error) {
	return client.New(c.config, options)
}

// RESTConfig returns the [rest.Config] for accessing this vCluster.
// NOTE: This config contains non-serializable values that are required for it
// to function properly.
func (c *Cluster) RESTConfig() *kube.RESTConfig {
	return rest.CopyConfig(c.config)
}

// PortForwardedRESTConfig returns a serializable [kube.RESTConfig] that allows
// external tools (helm, kubectl, etc) to connect to this vCluster.
//
// It starts a reverse proxy that's connected to the provided [context.Context]
// which forwards incoming traffic to the vCluster's kubeapi-server.
func (c *Cluster) PortForwardedRESTConfig(ctx context.Context) (*kube.RESTConfig, error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, errors.WithStack(err)
	}

	proxyCfg := c.RESTConfig()

	p := proxy{
		lis: lis,
		to: func() (net.Conn, error) {
			return proxyCfg.Dial(ctx, "tcp", proxyCfg.Host)
		},
	}

	go p.Run(ctx)

	cfg := c.RESTConfig()

	// Clear the dialer customizations and redirect to our proxy.
	cfg.Dial = nil
	cfg.Host = "https://" + lis.Addr().String()

	return cfg, nil
}

// Delete deletes this vCluster by deleting the Namespace it's deployed into.
func (c *Cluster) Delete() error {
	client, err := client.New(c.hostConfig, client.Options{})
	if err != nil {
		return err
	}

	return client.Delete(context.Background(), c.namespace)
}

// the functions below differ from our other helm and kubectl mechanisms since they leverage helm as
// a library rather than using the CLI, this is necessary for VCluster since we
// do a bunch of hole punching and proxying that can't be persisted to disk.

func (c *Cluster) SetScheme(scheme *runtime.Scheme) {
	c.scheme = scheme
}

func (c *Cluster) KubectlApply(ctx context.Context, manifest []byte) error {
	logger := log.FromContext(ctx)
	return c.doKubectl(ctx, manifest, func(k8sclient client.Client, decoded *unstructured.Unstructured) error {
		logger.Info("patching object", "name", decoded.GetName(), "namespace", decoded.GetNamespace(), "gvk", decoded.GroupVersionKind().String())
		return k8sclient.Patch(ctx, decoded, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))
	})
}

func (c *Cluster) KubectlDelete(ctx context.Context, manifest []byte) error {
	logger := log.FromContext(ctx)
	return c.doKubectl(ctx, manifest, func(k8sclient client.Client, decoded *unstructured.Unstructured) error {
		logger.Info("deleting object", "name", decoded.GetName(), "namespace", decoded.GetNamespace(), "gvk", decoded.GroupVersionKind().String())
		return k8sclient.Delete(ctx, decoded)
	})
}

func (c *Cluster) doKubectl(ctx context.Context, manifest []byte, fn func(k8sclient client.Client, decoded *unstructured.Unstructured) error) error {
	logger := log.FromContext(ctx)

	logger.Info("initializing client")
	k8sClient, err := c.Client(client.Options{Scheme: c.scheme})
	if err != nil {
		return err
	}
	return DecodeManifest(manifest, func(decoded *unstructured.Unstructured) error {
		if err := fn(k8sClient, decoded); err != nil {
			if !k8sapierrors.IsNotFound(err) {
				logger.Error(err, "error doing operation")
				return err
			}
			return nil
		}
		return nil
	})
}

func DecodeManifest(manifest []byte, fn func(decoded *unstructured.Unstructured) error) error {
	reader := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(manifest), 1024)
	for {
		var decoded unstructured.Unstructured
		if err := reader.Decode(&decoded); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if decoded.GroupVersionKind().Empty() {
			// ignore if no GVKs are set
			continue
		}

		if err := fn(&decoded); err != nil {
			return err
		}
	}
}

func (c *Cluster) HelmInstall(ctx context.Context, chartName string, options helm.InstallOptions) (*release.Release, error) {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(c.AsRESTClientGetter(), options.Namespace, "secret", log.FromContext(ctx).Info); err != nil {
		return nil, err
	}

	install := action.NewInstall(actionConfig)
	install.ReleaseName = options.Name
	install.Namespace = options.Namespace

	chart, err := loader.Load(chartName)
	if err != nil {
		return nil, err
	}

	return install.Run(chart, options.Values.(map[string]any))
}

func (c *Cluster) HelmUninstall(ctx context.Context, rel *release.Release) error {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(c.AsRESTClientGetter(), rel.Namespace, "secret", log.FromContext(ctx).Info); err != nil {
		return err
	}
	uninstall := action.NewUninstall(actionConfig)
	_, err := uninstall.Run(rel.Name)
	if err != nil && !strings.Contains(err.Error(), "release: not found") {
		return err
	}
	return nil
}

// proxy is a bad reverse TCP proxy to a single backend.
type proxy struct {
	lis net.Listener
	to  func() (net.Conn, error)
}

func (p *proxy) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = p.lis.Close()
	}()

	for {
		conn, err := p.lis.Accept()
		if err != nil {
			return err
		}

		outConn, err := p.to()
		if err != nil {
			return err
		}

		go pipe(ctx, conn, outConn)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// pipe is a bad [context.Context] aware and bi-directional version of
// [io.Copy].
// Don't expect great things.
func pipe(ctx context.Context, src net.Conn, dst net.Conn) error {
	errCh := make(chan error, 2)

	defer src.Close()
	defer dst.Close()

	go func() {
		_, err := io.Copy(dst, src)
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(src, dst)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case err := <-errCh:
		return err
	}
}
