package vcluster

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/k3d"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			// "controlPlane": map[string]any{
			// 	"distro": map[string]any{
			// 		"k3s": map[string]any{
			// 			"enabled": true,
			// 			"image": map[string]any{
			// 				"tag": "v1.29.6-k3s2",
			// 			},
			// 		},
			// 	},
			// },
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
