// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package bootstrap

import (
	"context"
	"strings"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RemoteConfiguration struct {
	KubeConfig     *rest.Config
	ContextName    string
	APIServer      string
	ServiceAddress string
}

func (r RemoteConfiguration) Client() (client.Client, error) {
	config, err := configFromContext(r.ContextName)
	if err != nil {
		return nil, err
	}

	return client.New(config, client.Options{})
}

func (r RemoteConfiguration) Config() (*rest.Config, error) {
	return configFromContext(r.ContextName)
}

func (r RemoteConfiguration) Address() (string, error) {
	if r.APIServer != "" {
		return r.APIServer, nil
	}
	config, err := r.Config()
	if err != nil {
		return "", err
	}

	return config.Host, nil
}

func (r RemoteConfiguration) FQDN(c BootstrapClusterConfiguration) (string, error) {
	if r.ServiceAddress != "" {
		return strings.Split(r.ServiceAddress, ":")[0], nil
	}

	return c.ServiceName + "-" + r.ContextName, nil
}

type BootstrapClusterConfiguration struct {
	BootstrapTLS         bool
	BootstrapKubeconfigs bool
	EnsureNamespace      bool
	OperatorNamespace    string
	ServiceName          string
	RemoteClusters       []RemoteConfiguration
}

func BootstrapKubernetesClusters(ctx context.Context, organization string, configuration BootstrapClusterConfiguration) error {
	caCertificate, err := GenerateCA(organization, "Root CA", nil)
	if err != nil {
		return err
	}

	kubeconfigs := [][]byte{}
	certificates := []*Certificate{}
	for _, cluster := range configuration.RemoteClusters {
		if configuration.BootstrapKubeconfigs {
			address, err := cluster.Address()
			if err != nil {
				return err
			}
			config, err := CreateRemoteKubeconfig(ctx, &RemoteKubernetesConfiguration{
				ContextName:     cluster.ContextName,
				EnsureNamespace: configuration.EnsureNamespace,
				Namespace:       configuration.OperatorNamespace,
				Name:            configuration.ServiceName,
				APIServer:       address,
				RESTConfig:      cluster.KubeConfig,
			})
			if err != nil {
				return err
			}
			kubeconfigs = append(kubeconfigs, config)
		}
		if configuration.BootstrapTLS {
			serviceFQDN, err := cluster.FQDN(configuration)
			if err != nil {
				return err
			}
			certificate, err := caCertificate.Sign(serviceFQDN)
			if err != nil {
				return err
			}
			certificates = append(certificates, certificate)
		}
	}

	for i, cluster := range configuration.RemoteClusters {
		if configuration.BootstrapKubeconfigs {
			for i := range kubeconfigs {
				kubeconfig := kubeconfigs[i]

				if err := CreateKubeconfigSecret(ctx, kubeconfig, &RemoteKubernetesConfiguration{
					ContextName:     cluster.ContextName,
					Namespace:       configuration.OperatorNamespace,
					Name:            configuration.ServiceName + "-" + configuration.RemoteClusters[i].ContextName,
					EnsureNamespace: configuration.EnsureNamespace,
					RESTConfig:      cluster.KubeConfig,
				}); err != nil {
					return err
				}
			}
		}
		if configuration.BootstrapTLS {
			certificate := certificates[i]
			if err := CreateTLSSecret(ctx, caCertificate, certificate, &RemoteKubernetesConfiguration{
				ContextName:     cluster.ContextName,
				Namespace:       configuration.OperatorNamespace,
				Name:            configuration.ServiceName,
				EnsureNamespace: configuration.EnsureNamespace,
				RESTConfig:      cluster.KubeConfig,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}
