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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/bootstrap"
)

// outputYAML is the value of --output that switches the command from
// applying resources to printing them as YAML on stdout.
const outputYAML = "yaml"

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
	// Output controls the command's mode. Empty (default) applies
	// resources to each remote cluster. "yaml" prints the manifests to
	// stdout and makes no cluster calls — for GitOps workflows that prefer
	// committing the artifacts and applying via Argo/Flux.
	Output string
	// OutputDir, when set together with Output="yaml", writes one YAML file
	// per cluster (<OutputDir>/<context>.yaml) instead of a single stream
	// on stdout. This is required for the TLS bootstrap path because each
	// cluster's tls.key is identity material that must not be applied to
	// any other cluster — a single multi-document stream cannot encode
	// that routing (comment headers are not honored by kubectl/GitOps
	// controllers).
	OutputDir string
}

// Run executes the bootstrap operation.
func (c *BootstrapConfig) Run(ctx context.Context, out io.Writer) error {
	switch c.Output {
	case "":
		// default flow: apply to clusters
	case outputYAML:
		return c.runYAML(out)
	default:
		return fmt.Errorf("unsupported --output value %q, only %q is supported", c.Output, outputYAML)
	}

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

// runYAML implements the --output=yaml mode: it generates the same
// resources Run would apply, but writes them as YAML instead of touching
// any cluster. When --output-dir is set, one file per cluster is written
// into that directory (<context>.yaml); otherwise resources are streamed
// to out grouped by comment headers.
//
// The TLS bootstrap path requires --output-dir because each cluster's
// generated tls.key is per-cluster identity material; a single stream
// would carry every peer's private key into whichever cluster the file
// is applied to. The LoadBalancer-only path allows stream output since
// it emits no secrets.
//
// Unlike the live-apply path, yaml mode does not require a kubeconfig or
// any cluster connectivity. Cluster names are derived from --context flags
// (LB mode) or --dns-override keys (bootstrap mode), and --name-override
// is applied the same way Resolve() would.
func (c *BootstrapConfig) runYAML(out io.Writer) error {
	if c.ProvisionLoadBalancers {
		return c.runYAMLLoadBalancers(out)
	}
	return c.runYAMLBootstrap(out)
}

// runYAMLLoadBalancers emits LoadBalancer Service manifests — step 1 of
// the GitOps workflow. The user applies these, waits for external IPs,
// then runs bootstrap --output=yaml with --dns-override to get TLS/NS
// manifests.
//
// Cluster names come from --context flags; no kubeconfig is needed.
func (c *BootstrapConfig) runYAMLLoadBalancers(out io.Writer) error {
	if len(c.Connection.Contexts) == 0 {
		return errors.New("--output=yaml --loadbalancer requires --context flags to specify cluster names")
	}

	nameOverrides, err := parseNameOverrides(c.Connection.NameOverrides)
	if err != nil {
		return err
	}

	remoteClusters := make([]bootstrap.RemoteConfiguration, len(c.Connection.Contexts))
	for i, name := range c.Connection.Contexts {
		prefix := name
		if override, ok := nameOverrides[name]; ok {
			prefix = override
		}
		remoteClusters[i] = bootstrap.RemoteConfiguration{
			ContextName: name,
			Name:        prefix,
		}
	}

	config := bootstrap.BootstrapClusterConfiguration{
		OperatorNamespace: c.Connection.Namespace,
		ServiceName:       c.Connection.ServiceName,
		RemoteClusters:    remoteClusters,
	}

	objsByCluster := bootstrap.GenerateLoadBalancerObjects(config)
	return c.writeYAMLOutput(out, objsByCluster)
}

// runYAMLBootstrap emits Namespace and TLS Secret manifests — step 2 of
// the GitOps workflow (or the only step when LB addresses are already known).
//
// Cluster names are derived from --dns-override keys; no kubeconfig is needed.
//
// --output-dir is required here: the TLS Secrets are per-cluster identity
// material, so a single multi-document stream cannot be safely applied
// to multiple destinations.
func (c *BootstrapConfig) runYAMLBootstrap(out io.Writer) error {
	if !c.TLS && !c.CreateNS {
		return errors.New("--output=yaml requires at least one of --tls or --create-namespace; otherwise no resources would be emitted")
	}

	dnsOverrides, err := parseDNSOverrides(c.DNSOverrides)
	if err != nil {
		return err
	}
	if len(dnsOverrides) == 0 {
		return errors.New("--output=yaml requires --dns-override for every cluster (LoadBalancer provisioning is disabled)")
	}
	if c.TLS && c.OutputDir == "" {
		return errors.New("--output=yaml with --tls requires --output-dir to write per-cluster files; a single stream would carry every cluster's tls.key into any destination it is applied to")
	}

	nameOverrides, err := parseNameOverrides(c.Connection.NameOverrides)
	if err != nil {
		return err
	}

	contexts := make([]string, 0, len(dnsOverrides))
	for name := range dnsOverrides {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)

	remoteClusters := make([]bootstrap.RemoteConfiguration, len(contexts))
	for i, name := range contexts {
		prefix := name
		if override, ok := nameOverrides[name]; ok {
			prefix = override
		}
		remoteClusters[i] = bootstrap.RemoteConfiguration{
			ContextName:    name,
			ServiceAddress: dnsOverrides[name],
			Name:           prefix,
		}
	}

	config := bootstrap.BootstrapClusterConfiguration{
		BootstrapTLS:      c.TLS,
		EnsureNamespace:   c.CreateNS,
		OperatorNamespace: c.Connection.Namespace,
		ServiceName:       c.Connection.ServiceName,
		RemoteClusters:    remoteClusters,
	}

	objsByCluster, err := bootstrap.GenerateBootstrapObjects(c.Organization, config)
	if err != nil {
		return fmt.Errorf("generating bootstrap objects: %w", err)
	}

	return c.writeYAMLOutput(out, objsByCluster)
}

// writeYAMLOutput dispatches to per-cluster files when OutputDir is set,
// otherwise streams the manifests to out grouped by comment headers.
// The stream form is only safe for resources that have no per-cluster
// identity material (Namespace, LoadBalancer Service).
func (c *BootstrapConfig) writeYAMLOutput(out io.Writer, objsByCluster map[string][]client.Object) error {
	if c.OutputDir != "" {
		return writeYAMLPerCluster(c.OutputDir, objsByCluster)
	}
	return writeYAMLStream(out, objsByCluster)
}

// writeYAMLPerCluster writes one YAML file per cluster into dir, named
// "<context>.yaml". Each file contains only that cluster's manifests, so
// applying it to the wrong cluster cannot leak per-cluster identity
// material across the trust boundary.
func writeYAMLPerCluster(dir string, objsByCluster map[string][]client.Object) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating output dir %s: %w", dir, err)
	}

	contexts := make([]string, 0, len(objsByCluster))
	for name := range objsByCluster {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)

	for _, name := range contexts {
		var buf bytes.Buffer
		for j, obj := range objsByCluster[name] {
			if j > 0 {
				if _, err := fmt.Fprintln(&buf, "---"); err != nil {
					return err
				}
			}
			data, err := marshalManifest(obj)
			if err != nil {
				return fmt.Errorf("marshalling object for %s: %w", name, err)
			}
			if _, err := buf.Write(data); err != nil {
				return err
			}
		}

		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func writeYAMLStream(out io.Writer, objsByCluster map[string][]client.Object) error {
	contexts := make([]string, 0, len(objsByCluster))
	for name := range objsByCluster {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)

	first := true
	for _, name := range contexts {
		objs := objsByCluster[name]
		if !first {
			if _, err := fmt.Fprintln(out, "---"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "# ===== Cluster: %s =====\n", name); err != nil {
			return err
		}
		for j, obj := range objs {
			if j > 0 {
				if _, err := fmt.Fprintln(out, "---"); err != nil {
					return err
				}
			}
			data, err := marshalManifest(obj)
			if err != nil {
				return fmt.Errorf("marshalling object for %s: %w", name, err)
			}
			if _, err := out.Write(data); err != nil {
				return err
			}
		}
		first = false
	}
	return nil
}

// marshalManifest renders a Kubernetes object as YAML and strips noise lines
// (creationTimestamp: null, status: {}, spec: {}) that sigs.k8s.io/yaml.Marshal
// emits for default-valued fields. The output is intended to be committed to
// git, so any line that conveys no information is worth removing.
func marshalManifest(obj client.Object) ([]byte, error) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return stripEmptyLines(data), nil
}

// NB: only safe for the object types we currently emit (Namespace, Secret,
// Service) — all have empty status/spec blocks. Revisit if new types are added.
func stripEmptyLines(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte("creationTimestamp: null")) ||
			bytes.Equal(trimmed, []byte("status: {}")) ||
			bytes.Equal(trimmed, []byte("status:")) ||
			bytes.Equal(trimmed, []byte("loadBalancer: {}")) ||
			bytes.Equal(trimmed, []byte("spec: {}")) {
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte("\n"))
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
printed on success for pasting into helm values.

--output=yaml skips applying anything and writes manifests as YAML. No
cluster contact or kubeconfig is needed — cluster names are derived from
--context flags (LB mode) or --dns-override keys (bootstrap mode).

When --output-dir <dir> is set, one file per cluster is written into the
directory as <context>.yaml. This is required for the TLS bootstrap path
because each cluster's tls.key is per-cluster identity material that
must not be applied to other clusters. The LoadBalancer-only step has
no secrets and additionally allows streaming to stdout (split on '---'
with comment headers).
Use this for GitOps pipelines that commit manifests and apply them via
Argo, Flux, or similar.

Two-step GitOps workflow:

  1. bootstrap --output=yaml --loadbalancer --context <ctx> ...
     Emits only LoadBalancer Service manifests. Apply them, wait for the
     cloud provider to assign external addresses.

  2. bootstrap --output=yaml --output-dir <dir> --dns-override ctx=<address> ...
     Emits Namespace and TLS Secret manifests into <dir>/<ctx>.yaml with
     the known addresses baked into the cert SANs.

When --loadbalancer is NOT set, --dns-override is required for every
cluster; cluster names are derived from the override keys.
ServiceAccount, SA token, and kubeconfig cache Secrets are not emitted
because the operator regenerates them at runtime.`,
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
    --tls --kubeconfigs=false

  # GitOps step 1: emit LoadBalancer Services (no kubeconfig needed)
  rpk k8s multicluster bootstrap \
    --context cluster-a --context cluster-b \
    --namespace redpanda \
    --loadbalancer \
    --output=yaml > multicluster-services.yaml

  # GitOps step 2: emit Namespace + TLS Secrets (no kubeconfig needed)
  rpk k8s multicluster bootstrap \
    --namespace redpanda \
    --dns-override cluster-a=cluster-a.example.com \
    --dns-override cluster-b=cluster-b.example.com \
    --output=yaml --output-dir ./multicluster-bootstrap`,
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
	cmd.Flags().StringVar(&cfg.Output, "output", "", `Output mode. Empty (default) applies resources to clusters. "yaml" emits manifests for GitOps and makes no cluster calls`)
	cmd.Flags().StringVar(&cfg.OutputDir, "output-dir", "", "With --output=yaml, write one file per cluster (<context>.yaml) into this directory. Required when emitting TLS Secrets; per-cluster routing prevents leaking peer identity keys across the trust boundary")

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
