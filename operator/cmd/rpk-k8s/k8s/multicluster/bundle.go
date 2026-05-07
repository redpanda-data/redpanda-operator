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
	"os"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// BundleConfig drives `rpk k8s multicluster bundle`.
//
// Default mode is single-starting-cluster: the user provides a kubeconfig
// (or a single context name) and the bundle command discovers peer clusters
// from labelled cache Secrets stored on that cluster by the operator's
// raft-bootstrap flow. This makes the tool usable when one of the peer
// clusters is down — pick any reachable one as the starting point and the
// rest of the roster falls out of the apiserver you can reach.
//
// Multi-context mode is also supported: pass --context multiple times (or
// pre-populate Connection.Connections in tests) to bypass discovery and
// diagnose exactly the listed clusters.
type BundleConfig struct {
	// Connection drives kubeconfig / context resolution. Reused from the
	// `status` and `bootstrap` commands.
	Connection ConnectionConfig
	// OutputPath is the destination zip file. Empty means
	// ./operator-bundle-<unix-ts>.zip in the current working directory.
	OutputPath string
	// IncludePrivateKeys disables the default redaction of TLS private keys
	// and cached peer kubeconfigs in serialised Secrets. Off by default
	// because Support tickets often involve emailing the bundle around.
	IncludePrivateKeys bool

	// SkipLogs disables operator pod log collection.
	SkipLogs bool
	// LogsSizeLimit is a human-readable byte limit for per-container logs
	// (e.g. "5M", "10MiB", "1G"). "0" disables the cap. Empty string means
	// the default ("5M"). Parsed once in Run via go-units.
	LogsSizeLimit string
	// LogsTailLines is the per-container tail-line cap. 0 disables the
	// cap. Negative values are treated as 0.
	LogsTailLines int64

	// ClusterChecks lets callers override the per-cluster check list. Nil
	// means use defaultClusterChecks (the same set the `status` command
	// runs). Tests can pass a smaller list to avoid checks whose timeouts
	// would dominate test wall-clock time.
	ClusterChecks []checks.ClusterCheck
	// CrossClusterChecks lets callers override the cross-cluster check
	// list. Nil means use defaultCrossClusterChecks.
	CrossClusterChecks []checks.CrossClusterCheck
	// LogFetcherFor returns the log fetcher used for a given cluster
	// connection. Nil means build a kubernetes.Interface-backed fetcher
	// from the connection's REST config. Tests use this to inject a stub
	// because envtest cannot return real container logs.
	LogFetcherFor func(ClusterConnection) (logFetcher, error)

	// Now overrides the clock for deterministic output in tests. Defaults
	// to time.Now.
	Now func() time.Time
}

// BundleResult exposes the aggregate state collected during a bundle run for
// programmatic inspection (mirrors StatusResult).
type BundleResult struct {
	// Path is the zip file the bundle was written to. Empty when Run wrote
	// to a non-file io.Writer.
	Path string
	// Contexts holds the per-cluster CheckContext after all checks ran.
	Contexts []*checks.CheckContext
	// ClusterResults[i] are the per-cluster check Results for Contexts[i].
	ClusterResults [][]checks.Result
	// CrossResults are the cross-cluster check Results.
	CrossResults []checks.Result
	// Errors collects non-fatal issues encountered during the run, in the
	// same form they're written to errors.txt inside the bundle.
	Errors []string
}

// BindFlags registers the bundle command flags on cmd.
func (c *BundleConfig) BindFlags(cmd *cobra.Command) {
	c.Connection.BindFlags(cmd)
	cmd.Flags().StringVarP(&c.OutputPath, "output", "o", "",
		"Path to write the bundle zip (default ./operator-bundle-<unix-ts>.zip)")
	cmd.Flags().BoolVar(&c.IncludePrivateKeys, "include-private-keys", false,
		"Include TLS private keys and cached peer kubeconfigs in serialised Secrets. Off by default.")
	cmd.Flags().BoolVar(&c.SkipLogs, "skip-logs", false,
		"Skip operator pod log collection")
	cmd.Flags().StringVar(&c.LogsSizeLimit, "logs-size-limit", "5M",
		`Per-container log byte cap (human-readable, e.g. "5M", "10MiB", "1G"; "0" disables the cap)`)
	cmd.Flags().Int64Var(&c.LogsTailLines, "logs-tail-lines", 5000,
		"Per-container log tail-line cap (0 disables the cap)")
}

// Run executes the bundle pipeline and writes the resulting zip to w. Returns
// a BundleResult for programmatic inspection. Per-cluster collection failures
// are recorded in BundleResult.Errors (and in errors.txt inside the zip)
// rather than returned, so a single bad cluster doesn't lose the whole
// bundle. Errors that prevent any output (e.g. resolving connections) are
// returned.
func (c *BundleConfig) Run(ctx context.Context, w io.Writer) (*BundleResult, error) {
	starting, err := c.Connection.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving starting connection: %w", err)
	}
	if len(starting) == 0 {
		return nil, fmt.Errorf("no starting connections resolved; pass --kubeconfig or --context")
	}

	roster := make([]ClusterConnection, 0, len(starting))
	roster = append(roster, starting...)
	var errs []string

	// Single-starting-cluster mode: discover peers from cache Secrets on
	// the one cluster the user gave us. Multi-context mode bypasses
	// discovery (the user is asserting the roster).
	if len(starting) == 1 {
		discovery, derr := discoverPeers(ctx, starting[0].Ctl, c.Connection.Namespace)
		if derr != nil {
			errs = append(errs, fmt.Sprintf("peer discovery failed: %v. The bundle covers only the starting cluster.", derr))
		}
		errs = append(errs, discovery.Warnings...)
		roster = append(roster, dedupeBySelf(discovery.Connections, starting[0].Name)...)
	}

	bw := newBundleWriter(w)
	defer bw.Close()

	clusterChecks := c.ClusterChecks
	if clusterChecks == nil {
		clusterChecks = defaultClusterChecks
	}
	crossClusterChecks := c.CrossClusterChecks
	if crossClusterChecks == nil {
		crossClusterChecks = defaultCrossClusterChecks
	}

	contexts := make([]*checks.CheckContext, len(roster))
	clusterResults := make([][]checks.Result, len(roster))
	for i, conn := range roster {
		cc := &checks.CheckContext{
			Context:      conn.Name,
			Namespace:    c.Connection.Namespace,
			ServiceName:  c.Connection.ServiceName,
			SecretPrefix: conn.SecretPrefix,
			Ctl:          conn.Ctl,
		}
		contexts[i] = cc
		clusterResults[i] = checks.RunClusterChecks(ctx, cc, clusterChecks)
	}
	crossResults := checks.RunCrossClusterChecks(contexts, crossClusterChecks)

	logsOpts, lerr := c.resolveLogsOptions()
	if lerr != nil {
		// A bad --logs-size-limit value is a configuration error worth
		// surfacing to the user before we produce a partial bundle.
		return nil, lerr
	}

	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if err := bw.writeManifestFile(c, contexts, now().UTC(), logsOpts); err != nil {
		errs = append(errs, fmt.Sprintf("writing manifest.json: %v", err))
	}
	if err := bw.writeStatusTable(contexts, clusterResults, crossResults); err != nil {
		errs = append(errs, fmt.Sprintf("writing status.txt: %v", err))
	}
	for i, cc := range contexts {
		for _, e := range bw.writeClusterArtifacts(cc, clusterResults[i], c.IncludePrivateKeys) {
			errs = append(errs, fmt.Sprintf("cluster %s: %v", cc.Context, e))
		}
	}

	// Phase 2: per-cluster operator pod logs. Skipped when --skip-logs is
	// set or when the per-cluster PodCheck didn't find a pod (cc.Pod is
	// nil). Per-container failures are recorded in errors.txt; the bundle
	// still completes.
	if !c.SkipLogs {
		for i, conn := range roster {
			fetcher, ferr := c.logFetcherFor(conn)
			if ferr != nil {
				errs = append(errs, fmt.Sprintf("cluster %s: building log fetcher: %v", conn.Name, ferr))
				continue
			}
			for _, e := range collectClusterLogs(ctx, bw, contexts[i], fetcher, logsOpts) {
				errs = append(errs, fmt.Sprintf("cluster %s: %v", conn.Name, e))
			}
		}
	}
	if err := bw.writeCrossClusterArtifacts(crossResults); err != nil {
		errs = append(errs, fmt.Sprintf("writing cross-cluster/checks.json: %v", err))
	}
	if err := bw.writeErrors(errs); err != nil {
		// errors.txt itself failed to write — surface to caller.
		return nil, fmt.Errorf("writing errors.txt: %w", err)
	}

	return &BundleResult{
		Contexts:       contexts,
		ClusterResults: clusterResults,
		CrossResults:   crossResults,
		Errors:         errs,
	}, nil
}

// dedupeBySelf removes any peer connection that has the same name as the
// starting cluster. Defends against the (legitimate) case where the user's
// own cache contains a self-entry; we take the starting connection as the
// authoritative one.
func dedupeBySelf(peers []ClusterConnection, selfName string) []ClusterConnection {
	out := peers[:0]
	for _, p := range peers {
		if p.Name == selfName {
			continue
		}
		out = append(out, p)
	}
	return out
}

// resolveLogsOptions parses LogsSizeLimit (a human-readable byte string) and
// returns the LogsOptions used for log retrieval. Empty LogsSizeLimit yields
// the package default (5 MiB / 5000 lines). Returns an error only when
// LogsSizeLimit is set but unparseable — bad input deserves a clear failure
// rather than a silently-large bundle.
func (c *BundleConfig) resolveLogsOptions() (LogsOptions, error) {
	defaults := defaultLogsOptions()
	out := LogsOptions{TailLines: c.LogsTailLines}
	if c.LogsTailLines == 0 {
		out.TailLines = defaults.TailLines
	} else if c.LogsTailLines < 0 {
		out.TailLines = 0
	}
	switch c.LogsSizeLimit {
	case "":
		out.LimitBytes = defaults.LimitBytes
	case "0":
		out.LimitBytes = 0
	default:
		n, err := parseLogsSize(c.LogsSizeLimit)
		if err != nil {
			return LogsOptions{}, fmt.Errorf("--logs-size-limit %q: %w", c.LogsSizeLimit, err)
		}
		out.LimitBytes = n
	}
	return out, nil
}

// parseLogsSize accepts both decimal SI suffixes ("5M", "10MB" → power-of-10)
// and binary IEC suffixes ("5Mi", "10MiB" → power-of-1024), matching the
// dual conventions users see in Kubernetes resource limits and in
// `rpk debug bundle --logs-size-limit`. Routes binary forms through
// units.RAMInBytes (which uses 1024-based units) and decimal forms through
// units.FromHumanSize (1000-based) — RAMInBytes is not used for decimal
// inputs because it would silently treat "5M" as 5 MiB.
func parseLogsSize(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	// IEC suffixes: anything ending in "i" (e.g. "5Mi") or "iB" (e.g. "5MiB").
	if strings.HasSuffix(trimmed, "i") || strings.HasSuffix(trimmed, "iB") {
		return units.RAMInBytes(trimmed)
	}
	return units.FromHumanSize(trimmed)
}

// logFetcherFor returns the logFetcher for a roster entry. When the caller
// configured a custom factory (test injection), that factory is used; the
// default builds a kubernetes.Interface-backed fetcher from the connection's
// REST config.
func (c *BundleConfig) logFetcherFor(conn ClusterConnection) (logFetcher, error) {
	if c.LogFetcherFor != nil {
		return c.LogFetcherFor(conn)
	}
	if conn.Ctl == nil {
		return nil, fmt.Errorf("connection %q has no kube.Ctl", conn.Name)
	}
	return newKubeLogFetcher(conn.Ctl.RestConfig())
}

func bundleCommand() *cobra.Command {
	var cfg BundleConfig

	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Collect a diagnostics bundle from a multicluster operator deployment",
		Long: `Collects environment data from each Kubernetes cluster running the
Redpanda multicluster operator and packages it into a ZIP file for support.
This is the operator-side counterpart to 'rpk debug bundle', which collects
data from the Redpanda brokers themselves.

By default, only one Kubernetes context is required. The bundle command
discovers peer clusters from labelled cache Secrets stored by the operator's
raft-bootstrap flow on the starting cluster, so the tool stays useful when
one of the peer clusters is down. Pass multiple --context flags to bypass
discovery and diagnose exactly that set.`,
		Example: `  # Single context — discover peers from the starting cluster
  rpk k8s multicluster bundle --kubeconfig /path/to/kubeconfig

  # Specific starting cluster from the default kubeconfig
  rpk k8s multicluster bundle --context cluster-a

  # Bypass discovery and bundle the listed clusters directly
  rpk k8s multicluster bundle --context cluster-a --context cluster-b --context cluster-c

  # Write to a specific path
  rpk k8s multicluster bundle --context cluster-a -o /tmp/operator-bundle.zip`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, file, err := cfg.openOutput()
			if err != nil {
				return err
			}
			defer file.Close()
			res, err := cfg.Run(cmd.Context(), file)
			if err != nil {
				return err
			}
			if res != nil {
				res.Path = path
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Operator bundle written to %s\n", path)
			if len(res.Errors) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(),
					"Bundle completed with %d non-fatal issue(s); see errors.txt inside the zip.\n",
					len(res.Errors),
				)
			}
			return nil
		},
	}

	cfg.BindFlags(cmd)
	return cmd
}

// openOutput resolves the output path (defaulting to a timestamped filename
// in the current working directory) and creates the file. The caller is
// responsible for closing the returned *os.File.
func (c *BundleConfig) openOutput() (string, *os.File, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	path := c.OutputPath
	if path == "" {
		path = fmt.Sprintf("operator-bundle-%d.zip", now().UTC().Unix())
	}
	f, err := os.Create(path)
	if err != nil {
		return "", nil, fmt.Errorf("creating output file %s: %w", path, err)
	}
	return path, f, nil
}
