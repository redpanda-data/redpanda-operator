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
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

// BootstrapConfig holds the configuration for the bootstrap command.
// It can be populated from CLI flags via the cobra command or set
// programmatically for testing.
type BootstrapConfig struct {
	Connection   ConnectionConfig
	Organization string
	DNSOverrides []string
	TLS          bool
	Kubeconfigs  bool
	CreateNS     bool
}

// Run executes the bootstrap operation.
func (c *BootstrapConfig) Run(ctx context.Context, out io.Writer) error {
	conns, err := c.Connection.Resolve()
	if err != nil {
		return err
	}

	dnsOverrides, err := parseDNSOverrides(c.DNSOverrides)
	if err != nil {
		return err
	}

	remoteClusters := make([]bootstrap.RemoteConfiguration, len(conns))
	for i, conn := range conns {
		remoteClusters[i] = bootstrap.RemoteConfiguration{
			KubeConfig:     conn.Ctl.RestConfig(),
			ContextName:    conn.Name,
			ServiceAddress: dnsOverrides[conn.Name],
			Name:           conn.SecretPrefix,
		}
	}

	config := bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:         c.TLS,
		BootstrapKubeconfigs: c.Kubeconfigs,
		EnsureNamespace:      c.CreateNS,
		OperatorNamespace:    c.Connection.Namespace,
		ServiceName:          c.Connection.ServiceName,
		RemoteClusters:       remoteClusters,
	}

	fmt.Fprintf(out, "Bootstrapping %d clusters...\n", len(remoteClusters))

	if err := bootstrap.BootstrapKubernetesClusters(ctx, c.Organization, config); err != nil {
		return fmt.Errorf("bootstrapping clusters: %w", err)
	}

	fmt.Fprintln(out, "Bootstrap complete.")
	return nil
}

func bootstrapCommand() *cobra.Command {
	cfg := BootstrapConfig{
		Organization: "Redpanda",
		TLS:          true,
		Kubeconfigs:  true,
		CreateNS:     true,
	}

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap a multicluster Redpanda deployment",
		Long: `Bootstrap TLS certificates and kubeconfig secrets across multiple
Kubernetes clusters for a Redpanda multicluster deployment.

This command connects to each specified Kubernetes context, generates a shared
CA certificate, and distributes per-cluster TLS certificates and kubeconfig
secrets so that the multicluster operator can communicate across clusters.

If --kubeconfig is provided, all contexts in the file are used automatically
and --context flags are not required. If both are provided, only the specified
contexts from the kubeconfig file are used.`,
		Example: `  # Bootstrap all clusters from a kubeconfig file
  rpk k8s multicluster bootstrap \
    --kubeconfig /path/to/kubeconfig \
    --namespace redpanda

  # Bootstrap specific contexts (uses default kubeconfig loading rules)
  rpk k8s multicluster bootstrap \
    --context cluster-a --context cluster-b --context cluster-c \
    --namespace redpanda

  # Override the TLS secret prefix when helm release names differ from context names
  rpk k8s multicluster bootstrap \
    --context cluster-a --context cluster-b --context cluster-c \
    --name-override cluster-a=redpanda-operator \
    --name-override cluster-b=redpanda-operator \
    --name-override cluster-c=redpanda-operator \
    --namespace redpanda

  # Override DNS names for TLS SANs on specific clusters
  rpk k8s multicluster bootstrap \
    --context cluster-a --context cluster-b \
    --dns-override cluster-a=cluster-a.example.com \
    --dns-override cluster-b=cluster-b.example.com \
    --namespace redpanda

  # Bootstrap only TLS certificates
  rpk k8s multicluster bootstrap \
    --kubeconfig /path/to/kubeconfig \
    --namespace redpanda \
    --tls --kubeconfigs=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Run(cmd.Context(), cmd.OutOrStdout())
		},
	}

	cfg.Connection.BindFlags(cmd)
	cmd.Flags().StringVar(&cfg.Organization, "organization", cfg.Organization, "Organization name for generated TLS certificates")
	cmd.Flags().StringArrayVar(&cfg.DNSOverrides, "dns-override", nil, "DNS override for TLS SANs in context=address format (repeatable)")
	cmd.Flags().BoolVar(&cfg.TLS, "tls", cfg.TLS, "Bootstrap TLS certificates")
	cmd.Flags().BoolVar(&cfg.Kubeconfigs, "kubeconfigs", cfg.Kubeconfigs, "Bootstrap kubeconfig secrets")
	cmd.Flags().BoolVar(&cfg.CreateNS, "create-namespace", cfg.CreateNS, "Create the namespace if it does not exist")

	return cmd
}

// parseDNSOverrides parses --dns-override flags in "context=address" format
// into a map keyed by context name.
func parseDNSOverrides(overrides []string) (map[string]string, error) {
	m := make(map[string]string, len(overrides))
	for _, o := range overrides {
		parts := strings.SplitN(o, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid --dns-override format %q, expected context=address", o)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}
