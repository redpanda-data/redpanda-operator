// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ClusterConnection holds the resolved client for a single cluster.
type ClusterConnection struct {
	Name string
	Ctl  *kube.Ctl
}

// ConnectionConfig holds the configuration for connecting to multiple k8s
// clusters. It can be populated from CLI flags via BindFlags or set
// programmatically for testing.
type ConnectionConfig struct {
	// Kubeconfig is the path to a kubeconfig file. When set, all contexts in
	// the file are used unless Contexts is also specified.
	Kubeconfig string
	// Contexts lists specific kubeconfig contexts to use. When Kubeconfig is
	// empty, these are resolved via the default kubeconfig loading rules.
	Contexts []string
	// Namespace is the namespace for operator resources.
	Namespace string
	// ServiceName is the operator service name used for label selection.
	ServiceName string

	// Connections is a pre-resolved list of cluster connections. When set,
	// Kubeconfig and Contexts are ignored. This allows tests to pass
	// kube.Ctl instances directly without writing kubeconfig files to disk.
	Connections []ClusterConnection
}

// BindFlags registers the connection flags on the given cobra command.
func (c *ConnectionConfig) BindFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.Kubeconfig, "kubeconfig", "", "Path to a kubeconfig file (all contexts in the file are used unless --context is also specified)")
	cmd.Flags().StringSliceVar(&c.Contexts, "context", nil, "Kubernetes contexts (repeatable; if omitted with --kubeconfig, all contexts in the file are used)")
	cmd.Flags().StringVar(&c.Namespace, "namespace", "redpanda", "Namespace for operator resources")
	cmd.Flags().StringVar(&c.ServiceName, "service-name", "redpanda-multicluster", "Service name for the multicluster operator")
}

// Resolve returns ClusterConnections from the config. If Connections is
// already set (programmatic use), it is returned directly. Otherwise,
// connections are built from Kubeconfig/Contexts flags.
func (c *ConnectionConfig) Resolve() ([]ClusterConnection, error) {
	if len(c.Connections) > 0 {
		return c.Connections, nil
	}
	if c.Kubeconfig == "" && len(c.Contexts) == 0 {
		return nil, fmt.Errorf("either --kubeconfig or --context must be specified")
	}
	if c.Kubeconfig != "" {
		return connectionsFromKubeconfig(c.Kubeconfig, c.Contexts)
	}
	return connectionsFromContexts(c.Contexts)
}

func connectionsFromKubeconfig(path string, contexts []string) ([]ClusterConnection, error) {
	config, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig from %s: %w", path, err)
	}

	if len(contexts) > 0 {
		for _, name := range contexts {
			if _, ok := config.Contexts[name]; !ok {
				return nil, fmt.Errorf("context %q not found in kubeconfig %s", name, path)
			}
		}
	} else {
		for name := range config.Contexts {
			contexts = append(contexts, name)
		}
	}

	if len(contexts) == 0 {
		return nil, fmt.Errorf("no contexts found in kubeconfig %s", path)
	}

	conns := make([]ClusterConnection, 0, len(contexts))
	for _, name := range contexts {
		rc, err := configFromAPIConfig(config, name)
		if err != nil {
			return nil, fmt.Errorf("building REST config for context %s: %w", name, err)
		}
		ctl, err := kube.FromRESTConfig(rc)
		if err != nil {
			return nil, fmt.Errorf("building kube.Ctl for context %s: %w", name, err)
		}
		conns = append(conns, ClusterConnection{Name: name, Ctl: ctl})
	}
	return conns, nil
}

func connectionsFromContexts(contexts []string) ([]ClusterConnection, error) {
	conns := make([]ClusterConnection, 0, len(contexts))
	for _, name := range contexts {
		rc, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{CurrentContext: name},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("building REST config for context %s: %w", name, err)
		}
		ctl, err := kube.FromRESTConfig(rc)
		if err != nil {
			return nil, fmt.Errorf("building kube.Ctl for context %s: %w", name, err)
		}
		conns = append(conns, ClusterConnection{Name: name, Ctl: ctl})
	}
	return conns, nil
}

func configFromAPIConfig(config *clientcmdapi.Config, contextName string) (*kube.RESTConfig, error) {
	return clientcmd.NewNonInteractiveClientConfig(
		*config,
		contextName,
		&clientcmd.ConfigOverrides{CurrentContext: contextName},
		nil,
	).ClientConfig()
}
