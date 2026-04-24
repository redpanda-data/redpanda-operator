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
	"time"

	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

// BootstrapConfig holds the configuration for the bootstrap command.
// It can be populated from CLI flags via the cobra command or set
// programmatically for testing.
type BootstrapConfig struct {
	Connection             ConnectionConfig
	Organization           string
	DNSOverrides           []string
	TLS                    bool
	Kubeconfigs            bool
	CreateNS               bool
	ProvisionLoadBalancers bool
	LoadBalancerTimeout    time.Duration
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

	// Provision peer LoadBalancers BEFORE signing TLS certs so the
	// external address each cluster publishes ends up in the cert's
	// SAN list. Running this at the CLI layer (rather than through
	// BootstrapClusterConfiguration.ProvisionLoadBalancers) keeps the
	// interactive spinner out of the library and lets programmatic
	// callers drive their own progress UI.
	if c.ProvisionLoadBalancers {
		lbCfg := bootstrap.PeerLoadBalancerConfig{
			ProvisionTimeout: c.LoadBalancerTimeout,
		}
		fmt.Fprintf(out, "Provisioning peer LoadBalancers for %d clusters...\n", len(remoteClusters))
		for i := range config.RemoteClusters {
			cluster := config.RemoteClusters[i]
			if cluster.ServiceAddress != "" {
				fmt.Fprintf(out, "  [%s] using provided address %s (skipping LoadBalancer)\n",
					cluster.ContextName, cluster.ServiceAddress)
				continue
			}
			address, err := provisionWithSpinner(ctx, out, cluster, config, lbCfg)
			if err != nil {
				return fmt.Errorf("provisioning LoadBalancer on %s: %w", cluster.ContextName, err)
			}
			config.RemoteClusters[i].ServiceAddress = address
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "Bootstrapping %d clusters...\n", len(remoteClusters))

	if err := bootstrap.BootstrapKubernetesClusters(ctx, c.Organization, config); err != nil {
		return fmt.Errorf("bootstrapping clusters: %w", err)
	}

	fmt.Fprintln(out, "Bootstrap complete.")

	if c.ProvisionLoadBalancers {
		printPeersBlock(out, config.RemoteClusters)
	}
	return nil
}

// provisionWithSpinner calls bootstrap.EnsurePeerLoadBalancer with an
// interactive spinner attached. On success the spinner is replaced with
// a "✓ <cluster> -> <address>" line; on failure it's replaced with "✗".
func provisionWithSpinner(
	ctx context.Context,
	out io.Writer,
	cluster bootstrap.RemoteConfiguration,
	config bootstrap.BootstrapClusterConfiguration,
	lbCfg bootstrap.PeerLoadBalancerConfig,
) (string, error) {
	sp := newSpinner(out, fmt.Sprintf("[%s] waiting for LoadBalancer", cluster.ContextName))
	sp.Start()

	address, err := bootstrap.EnsurePeerLoadBalancer(ctx, cluster, config, lbCfg)
	if err != nil {
		sp.Stop(fmt.Sprintf("✗ [%s] %v", cluster.ContextName, err))
		return "", err
	}
	sp.Stop(fmt.Sprintf("✓ [%s] %s", cluster.ContextName, address))
	return address, nil
}

// printPeersBlock writes a ready-to-paste helm peers block using the
// provisioned addresses. The block matches the shape the operator chart
// expects under multicluster.peers so the user can copy it straight into
// their values file.
func printPeersBlock(out io.Writer, clusters []bootstrap.RemoteConfiguration) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Use these as multicluster.peers in your helm values:")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  multicluster:")
	fmt.Fprintln(out, "    peers:")
	for _, c := range clusters {
		name := c.Name
		if name == "" {
			name = c.ContextName
		}
		fmt.Fprintf(out, "      - name: %s\n", name)
		fmt.Fprintf(out, "        address: %s\n", c.ServiceAddress)
	}
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
contexts from the kubeconfig file are used.

--loadbalancer provisions a standalone LoadBalancer Service on each cluster
before signing certificates, waits for the cloud provider to assign an
external address, and bakes that address into the cert SANs. This resolves
the deploy/redeploy cycle that otherwise forces a first helm install just
to learn each cluster's external IP/hostname. The resulting peer list is
printed on success for pasting into helm values.`,
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

  # Provision LoadBalancer Services and use their addresses for cert SANs
  rpk k8s multicluster bootstrap \
    --kubeconfig /path/to/kubeconfig \
    --namespace redpanda \
    --loadbalancer

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
	cmd.Flags().BoolVar(&cfg.ProvisionLoadBalancers, "loadbalancer", cfg.ProvisionLoadBalancers, "Provision a standalone LoadBalancer Service per cluster and use its external address for TLS SANs")
	cmd.Flags().DurationVar(&cfg.LoadBalancerTimeout, "loadbalancer-timeout", 0, "Per-cluster timeout waiting for a LoadBalancer address (0 = default of 10m)")

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
