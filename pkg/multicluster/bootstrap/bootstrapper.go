// Copyright 2026 Redpanda Data, Inc.
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
	// Name is the helm fullname of the operator on this cluster, used as the
	// TLS secret name prefix (<Name>-multicluster-certificates). When empty,
	// BootstrapClusterConfiguration.ServiceName is used as the fallback.
	Name string
}

func (r RemoteConfiguration) Client() (client.Client, error) {
	config, err := r.Config()
	if err != nil {
		return nil, err
	}

	return client.New(config, client.Options{})
}

func (r RemoteConfiguration) Config() (*rest.Config, error) {
	if r.KubeConfig != nil {
		return r.KubeConfig, nil
	}
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
	// Use the per-cluster helm fullname as the FQDN base when available,
	// falling back to the global ServiceName. The ContextName disambiguates
	// between clusters that share the same fullname.
	base := r.Name
	if base == "" {
		base = c.ServiceName
	}
	return base + "-" + r.ContextName, nil
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
			tlsName := cluster.Name
			if tlsName == "" {
				tlsName = configuration.ServiceName
			}
			certificate := certificates[i]
			if err := CreateTLSSecret(ctx, caCertificate, certificate, &RemoteKubernetesConfiguration{
				ContextName:     cluster.ContextName,
				Namespace:       configuration.OperatorNamespace,
				Name:            tlsName,
				EnsureNamespace: configuration.EnsureNamespace,
				RESTConfig:      cluster.KubeConfig,
			}); err != nil {
				return err
			}
		}
	}

	return nil
}
