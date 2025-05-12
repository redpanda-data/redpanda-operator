package vcluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

const (
	// vClusterChartVersion is the pinned version of the vCluster helm chart. It's
	// pinned to avoid sudden failures if there are backwards incompatible changes
	// added.
	vClusterChartVersion    = "v0.23.0"
	certManagerChartversion = "v1.8.0"
)

type Cluster struct {
	config    *kube.RESTConfig
	helm      *helm.Client
	release   helm.Release
	host      *k3d.Cluster
	namespace *corev1.Namespace
}

func New(ctx context.Context, host *k3d.Cluster) (*Cluster, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, time.Minute, errors.New("vCluster creation timed out"))
	defer cancel()

	c, err := client.New(host.RESTConfig(), client.Options{})
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
		KubeConfig: host.RESTConfig(),
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
	dialer := kube.NewPodDialer(host.RESTConfig())

	cfg.Dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		// It's fairly safe to assume that all connections are meant for the
		// API server as this function is only accessible via the vCluster's
		// rest config.
		idx := strings.LastIndex(address, ":")
		return dialer.DialContext(ctx, network, fmt.Sprintf("%s-0.%s:%s", rel.Name, rel.Namespace, address[idx+1:]))
	}

	return &Cluster{
		config:    cfg,
		helm:      hc,
		release:   rel,
		host:      host,
		namespace: namespace,
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

// Host returns the host Cluster this vCluster is deployed onto.
func (c *Cluster) Host() *k3d.Cluster {
	return c.host
}

// Delete deletes this vCluster by deleting the Namespace it's deployed into.
func (c *Cluster) Delete() error {
	client, err := client.New(c.host.RESTConfig(), client.Options{})
	if err != nil {
		return err
	}

	return client.Delete(context.Background(), c.namespace)
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
