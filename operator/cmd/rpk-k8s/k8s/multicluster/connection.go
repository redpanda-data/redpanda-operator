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
	"strings"

	"github.com/redpanda-data/common-go/kube"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ClusterConnection holds the resolved client for a single cluster.
type ClusterConnection struct {
	Name string
	Ctl  *kube.Ctl
	// SecretPrefix is the helm fullname of the operator on this cluster,
	// used to derive the TLS secret name (<SecretPrefix>-multicluster-certificates).
	// Defaults to Name (the kubernetes context name) when not set.
	SecretPrefix string
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
	// ServiceName is the operator service name used for label selection
	// (app.kubernetes.io/name). Typically "operator" for the redpanda operator chart.
	ServiceName string
	// NameOverrides maps context names to their helm fullname override, in
	// "context=prefix" format. The prefix is used to derive the TLS secret
	// name (<prefix>-multicluster-certificates). Defaults to the context name.
	NameOverrides []string

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
	cmd.Flags().StringVar(&c.ServiceName, "service-name", "operator", "Operator deployment label selector value (app.kubernetes.io/name)")
	cmd.Flags().StringArrayVar(&c.NameOverrides, "name-override", nil, "Override the TLS secret prefix for a context in context=prefix format (repeatable; defaults to the context name)")
}

// Resolve returns ClusterConnections from the config. If Connections is
// already set (programmatic use), it is returned directly (with SecretPrefix
// defaulted to Name for any connection that has no prefix set). Otherwise,
// connections are built from Kubeconfig/Contexts flags and NameOverrides
// are applied.
func (c *ConnectionConfig) Resolve() ([]ClusterConnection, error) {
	nameOverrides, err := parseNameOverrides(c.NameOverrides)
	if err != nil {
		return nil, err
	}

	if len(c.Connections) > 0 {
		conns := make([]ClusterConnection, len(c.Connections))
		copy(conns, c.Connections)
		for i, conn := range conns {
			if conn.SecretPrefix == "" {
				if override, ok := nameOverrides[conn.Name]; ok {
					conns[i].SecretPrefix = override
				} else {
					conns[i].SecretPrefix = conn.Name
				}
			}
		}
		return conns, nil
	}

	if c.Kubeconfig == "" && len(c.Contexts) == 0 {
		return nil, fmt.Errorf("either --kubeconfig or --context must be specified")
	}

	var conns []ClusterConnection
	if c.Kubeconfig != "" {
		conns, err = connectionsFromKubeconfig(c.Kubeconfig, c.Contexts)
	} else {
		conns, err = connectionsFromContexts(c.Contexts)
	}
	if err != nil {
		return nil, err
	}

	for i, conn := range conns {
		if override, ok := nameOverrides[conn.Name]; ok {
			conns[i].SecretPrefix = override
		} else {
			conns[i].SecretPrefix = conn.Name
		}
	}
	return conns, nil
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

// parseNameOverrides parses --name-override flags in "context=prefix" format
// into a map keyed by context name.
func parseNameOverrides(overrides []string) (map[string]string, error) {
	m := make(map[string]string, len(overrides))
	for _, o := range overrides {
		parts := strings.SplitN(o, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --name-override format %q, expected context=prefix", o)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}
