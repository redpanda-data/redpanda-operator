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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	clusterchecks "github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks/cluster"
)

// CheckConfig holds the configuration for the check command.
type CheckConfig struct {
	Connection ConnectionConfig
	Name       string // StretchCluster resource name
}

// Per-cluster checks run in order. Later checks may depend on state
// populated by earlier ones via CheckContext.
var defaultClusterClusterChecks = []clusterchecks.ClusterCheck{
	&clusterchecks.ResourceCheck{},
	&clusterchecks.ConditionsCheck{},
	&clusterchecks.PoolsCheck{},
	&clusterchecks.SecretsCheck{},
	&clusterchecks.BrokerHealthCheck{},
}

// Cross-cluster checks run once after all per-cluster checks complete.
var defaultClusterCrossClusterChecks = []clusterchecks.CrossClusterCheck{
	&clusterchecks.SpecConsistencyCheck{},
	&clusterchecks.StatusAgreementCheck{},
	&clusterchecks.BootstrapSecretConsistencyCheck{},
	&clusterchecks.CASecretConsistencyCheck{},
}

// CheckResult holds the full output of a check run.
type CheckResult struct {
	Contexts       []*clusterchecks.CheckContext
	ClusterResults [][]clusterchecks.Result
	CrossResults   []clusterchecks.Result
}

// Run executes the cluster checks and writes formatted output to out.
func (c *CheckConfig) Run(ctx context.Context, out io.Writer) (*CheckResult, error) {
	conns, err := c.Connection.Resolve()
	if err != nil {
		return nil, err
	}

	// Register the StretchCluster CRD scheme on each connection's client
	// so we can read StretchCluster resources.
	for _, conn := range conns {
		if err := redpandav1alpha2.AddToScheme(conn.Ctl.Scheme()); err != nil {
			return nil, fmt.Errorf("registering scheme for context %s: %w", conn.Name, err)
		}
	}

	contexts := make([]*clusterchecks.CheckContext, len(conns))
	clusterResults := make([][]clusterchecks.Result, len(conns))

	for i, conn := range conns {
		cc := &clusterchecks.CheckContext{
			Context:   conn.Name,
			Namespace: c.Connection.Namespace,
			Name:      c.Name,
			Ctl:       conn.Ctl,
		}
		contexts[i] = cc
		clusterResults[i] = clusterchecks.RunClusterChecks(ctx, cc, defaultClusterClusterChecks)
	}

	crossResults := clusterchecks.RunCrossClusterChecks(contexts, defaultClusterCrossClusterChecks)

	printCheckHeader(out, c.Name, c.Connection.Namespace, len(conns))
	printConditionsTable(out, contexts)
	printPoolsTable(out, contexts)
	printCheckResults(out, contexts, clusterResults)
	printCrossClusterCheckResults(out, crossResults)

	return &CheckResult{
		Contexts:       contexts,
		ClusterResults: clusterResults,
		CrossResults:   crossResults,
	}, nil
}

func checkCommand() *cobra.Command {
	var cfg CheckConfig

	cmd := &cobra.Command{
		Use:   "check <name>",
		Short: "Check the health of a StretchCluster and its Redpanda brokers",
		Long: `Checks the health of a StretchCluster resource across all participating
Kubernetes clusters. Inspects status conditions, node pool readiness,
cross-cluster spec consistency, and shared secret integrity.

Connects to each specified Kubernetes context, reads the StretchCluster
resource and its associated secrets, and validates consistency.`,
		Example: `  # Check a StretchCluster across all clusters in a kubeconfig
  rpk k8s multicluster check my-cluster --kubeconfig /path/to/kubeconfig

  # Check specific clusters in a specific namespace
  rpk k8s multicluster check my-cluster \
    --context cluster-a --context cluster-b --context cluster-c \
    --namespace redpanda`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Name = args[0]
			_, err := cfg.Run(cmd.Context(), cmd.OutOrStdout())
			return err
		},
	}

	cfg.Connection.BindFlags(cmd)

	return cmd
}

func printCheckHeader(w io.Writer, name, namespace string, clusters int) {
	fmt.Fprintf(w, "STRETCH CLUSTER: %s  NAMESPACE: %s  CLUSTERS: %d\n", name, namespace, clusters)
}

func printConditionsTable(w io.Writer, contexts []*clusterchecks.CheckContext) {
	// Use conditions from the first cluster that has them.
	var conditions []metav1.Condition
	for _, cc := range contexts {
		if len(cc.Conditions) > 0 {
			conditions = cc.Conditions
			break
		}
	}
	if len(conditions) == 0 {
		return
	}

	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CONDITION\tSTATUS\tREASON\tMESSAGE")
	for _, cond := range conditions {
		msg := cond.Message
		if len(msg) > 80 {
			msg = msg[:77] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", cond.Type, cond.Status, cond.Reason, msg)
	}
	_ = tw.Flush()
}

func printPoolsTable(w io.Writer, contexts []*clusterchecks.CheckContext) {
	// Collect all pools across all clusters.
	type poolEntry struct {
		context string
		pool    redpandav1alpha2.EmbeddedNodePoolStatus
	}
	var entries []poolEntry
	for _, cc := range contexts {
		for _, pool := range cc.NodePools {
			entries = append(entries, poolEntry{context: cc.Context, pool: pool})
		}
	}
	if len(entries) == 0 {
		return
	}

	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CLUSTER\tPOOL\tDESIRED\tREPLICAS\tUP-TO-DATE\tOUT-OF-DATE\tDECOMMISSIONING")
	for _, e := range entries {
		name := e.pool.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\n",
			e.context, name,
			e.pool.DesiredReplicas, e.pool.Replicas,
			e.pool.UpToDateReplicas, e.pool.OutOfDateReplicas,
			e.pool.CondemnedReplicas)
	}
	_ = tw.Flush()
}

func printCheckResults(w io.Writer, contexts []*clusterchecks.CheckContext, results [][]clusterchecks.Result) {
	// Skip the conditions check results in the per-cluster section since
	// they're already shown in the conditions table. Only show non-pass
	// results from other checks.
	hasIssues := false
	for _, rs := range results {
		for _, r := range rs {
			if r.Status == "fail" && r.Name != "conditions" {
				hasIssues = true
				break
			}
		}
		if hasIssues {
			break
		}
	}
	if !hasIssues {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "ISSUES:")
	for i, rs := range results {
		for _, r := range rs {
			if r.Name == "conditions" {
				continue
			}
			if r.Status == "fail" {
				fmt.Fprintf(w, "  ✗ %s: [%s] %s\n", contexts[i].Context, r.Name, r.Message)
			}
		}
	}
}

func printCrossClusterCheckResults(w io.Writer, results []clusterchecks.Result) {
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CROSS-CLUSTER:")
	for _, r := range results {
		switch r.Status {
		case "pass":
			fmt.Fprintf(w, "  ✓ [%s] %s\n", r.Name, r.Message)
		case "fail":
			fmt.Fprintf(w, "  ✗ [%s] %s\n", r.Name, r.Message)
		case "skip":
			fmt.Fprintf(w, "  - [%s] %s\n", r.Name, r.Message)
		}
	}
}
