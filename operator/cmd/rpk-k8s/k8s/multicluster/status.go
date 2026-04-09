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
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// StatusConfig holds the configuration for the status command.
// It can be populated from CLI flags via the cobra command or set
// programmatically for testing.
type StatusConfig struct {
	Connection ConnectionConfig
}

// Per-cluster checks run in order. Later checks may depend on state
// populated by earlier ones via CheckContext.
var defaultClusterChecks = []checks.ClusterCheck{
	&checks.PodCheck{},
	&checks.DeploymentCheck{},
	&checks.TLSCheck{},
	&checks.RaftCheck{},
	&checks.TLSSANCheck{},
	&checks.DeploymentRaftCheck{},
}

// Cross-cluster checks run once after all per-cluster checks complete.
var defaultCrossClusterChecks = []checks.CrossClusterCheck{
	&checks.UniqueNamesCheck{},
	&checks.PeerAgreementCheck{},
	&checks.LeaderAgreementCheck{},
	&checks.CAConsistencyCheck{},
}

// StatusResult holds the full output of a status run, for programmatic
// inspection by tests.
type StatusResult struct {
	Contexts       []*checks.CheckContext
	ClusterResults [][]checks.Result
	CrossResults   []checks.Result
}

// Run executes the status checks and writes formatted output to out.
// Returns the full result for programmatic inspection.
func (c *StatusConfig) Run(ctx context.Context, out io.Writer) (*StatusResult, error) {
	conns, err := c.Connection.Resolve()
	if err != nil {
		return nil, err
	}

	contexts := make([]*checks.CheckContext, len(conns))
	clusterResults := make([][]checks.Result, len(conns))

	for i, conn := range conns {
		cc := &checks.CheckContext{
			Context:     conn.Name,
			Namespace:   c.Connection.Namespace,
			ServiceName: c.Connection.ServiceName,
			Ctl:         conn.Ctl,
		}
		contexts[i] = cc
		clusterResults[i] = checks.RunClusterChecks(ctx, cc, defaultClusterChecks)
	}

	crossResults := checks.RunCrossClusterChecks(contexts, defaultCrossClusterChecks)

	printStatusTable(out, contexts)
	printClusterResults(out, contexts, clusterResults)
	printCrossClusterResults(out, crossResults)

	return &StatusResult{
		Contexts:       contexts,
		ClusterResults: clusterResults,
		CrossResults:   crossResults,
	}, nil
}

func statusCommand() *cobra.Command {
	var cfg StatusConfig

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check the health of a multicluster operator deployment",
		Long: `Checks each cluster's operator pod health, raft consensus state,
TLS certificate validity, and cross-cluster consistency.

Connects to each specified Kubernetes context, finds the multicluster operator
pod, port-forwards to its gRPC transport, and queries raft status. Also
inspects Deployment configuration and TLS secrets for correctness.`,
		Example: `  # Check status across all clusters in a kubeconfig
  rpk k8s multicluster status --kubeconfig /path/to/kubeconfig

  # Check specific clusters
  rpk k8s multicluster status \
    --context cluster-a --context cluster-b --context cluster-c

  # Check with custom namespace and service name
  rpk k8s multicluster status \
    --context cluster-a --context cluster-b \
    --namespace redpanda --service-name redpanda-multicluster`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cfg.Run(cmd.Context(), cmd.OutOrStdout())
			return err
		},
	}

	cfg.Connection.BindFlags(cmd)

	return cmd
}

func printStatusTable(w io.Writer, contexts []*checks.CheckContext) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CLUSTER\tOPERATOR\tRAFT-STATE\tLEADER\tPEERS\tUNHEALTHY\tTLS\tSECRETS")

	for _, cc := range contexts {
		operator := "-"
		if cc.Pod != nil {
			operator = string(cc.Pod.Status.Phase)
			var restarts int32
			for _, cs := range cc.Pod.Status.ContainerStatuses {
				restarts += cs.RestartCount
			}
			if restarts > 0 {
				operator = fmt.Sprintf("%s(%d)", operator, restarts)
			}
		}

		raftState, leader, peers, unhealthy := "-", "-", "-", "-"
		if cc.RaftStatus != nil {
			raftState = cc.RaftStatus.RaftState
			leader = cc.RaftStatus.Leader
			if leader == "" {
				leader = "(none)"
			}
			peers = fmt.Sprintf("%d", len(cc.RaftStatus.ClusterNames))
			unhealthy = fmt.Sprintf("%d", len(cc.RaftStatus.UnhealthyPeers))
		}

		tlsStatus := "-"
		if cc.CACert != nil {
			tlsStatus = "ok"
		}

		secrets := "-"
		if cc.TLSSecret != nil {
			secrets = "ok"
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			cc.Context, operator, raftState, leader, peers, unhealthy, tlsStatus, secrets)
	}
	_ = tw.Flush()
}

func printClusterResults(w io.Writer, contexts []*checks.CheckContext, results [][]checks.Result) {
	hasFailures := false
	for _, rs := range results {
		for _, r := range rs {
			if !r.OK {
				hasFailures = true
				break
			}
		}
		if hasFailures {
			break
		}
	}
	if !hasFailures {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "ISSUES:")
	for i, rs := range results {
		for _, r := range rs {
			if !r.OK {
				fmt.Fprintf(w, "  ✗ %s: [%s] %s\n", contexts[i].Context, r.Name, r.Message)
			}
		}
	}
}

func printCrossClusterResults(w io.Writer, results []checks.Result) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CROSS-CLUSTER:")
	for _, r := range results {
		if r.OK {
			fmt.Fprintf(w, "  ✓ [%s] %s\n", r.Name, r.Message)
		} else {
			fmt.Fprintf(w, "  ✗ [%s] %s\n", r.Name, r.Message)
		}
	}
}
