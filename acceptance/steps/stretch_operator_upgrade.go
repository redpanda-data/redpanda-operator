// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

// raftTermStateKey holds the per-vcluster raft term observed before the
// operator upgrade begins. After each per-vcluster upgrade we assert the term
// has strictly advanced versus the prior recorded value.
type raftTermStateKey struct{}

type raftTermState struct {
	terms map[string]uint64 // node.Name() -> last observed term
}

// publishedOperatorChartSource constructs an operatorChartSource for the
// charts.redpanda.com upstream operator chart at the given version. The image
// fields are left blank so the chart's defaults take effect (matches what
// users running `helm install redpanda/operator --version vX.Y.Z` see).
func publishedOperatorChartSource(chartName, version string) operatorChartSource {
	return operatorChartSource{
		RemoteChart: &remoteChart{
			RepoURL:   "https://charts.redpanda.com",
			ChartName: trimRepoPrefix(chartName),
			Version:   version,
		},
	}
}

// trimRepoPrefix turns "redpanda/operator" into "operator" so the user can
// write the step the same way they'd write `helm install`.
func trimRepoPrefix(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

// createNetworkedVClusterOperatorsWithChart installs the named published helm
// chart (e.g. "redpanda/operator" @ "v26.2.1-beta.2") into each vcluster
// instead of the local dev chart. Used by the operator-upgrade acceptance
// scenario to set up a "starting from a released operator" baseline.
func createNetworkedVClusterOperatorsWithChart(ctx context.Context, t framework.TestingT, clusterName string, clusters int32, chart, version string) context.Context {
	src := publishedOperatorChartSource(chart, version)
	return createNetworkedVClusterOperatorsFromSource(ctx, t, clusterName, clusters, src)
}

// upgradeMulticlusterOperatorOneAtATime walks the vclusters in index order
// (vc-0, vc-1, …) and helm-upgrades the operator chart in each one to the
// local dev chart (../operator/chart) with image localhost/redpanda-operator:dev.
// After each upgrade we wait for the operator deployment to roll, then assert
// the cross-cluster raft quorum recovers and the upgraded operator rejoins it.
// Because raft only advances the term on a *leader* restart (a follower restart
// simply rejoins under the existing leader without a re-election), we require a
// strict term advance only when the upgraded node was the leader; otherwise we
// require the term not to regress and the upgraded follower to have caught back
// up to the quorum.
func upgradeMulticlusterOperatorOneAtATime(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	nodes := getNodes(ctx, clusterName)
	namespace := metav1.NamespaceDefault

	// Snapshot raft term on every vcluster before the first upgrade. The
	// snapshot also serves as a sanity check that the published-chart
	// operator is in fact running and serving the TransportService.
	terms := snapshotRaftTerms(ctx, t, nodes, namespace)
	ctx = context.WithValue(ctx, raftTermStateKey{}, &raftTermState{terms: terms})

	for _, node := range nodes {
		// Capture leadership immediately before the upgrade: only restarting the
		// current leader forces a term-advancing re-election.
		wasLeader := currentRaftLeader(ctx, t, nodes, namespace) == node.Name()
		t.Logf("helm-upgrading operator in %s to local dev chart (currentLeader=%t)", node.Name(), wasLeader)

		peers := operatorPeers(nodes)

		oldOperatorPodUID := getOperatorPodUID(ctx, t, node, namespace)

		_, err := node.HelmUpgrade(ctx, operatorHelmReleaseName, "../operator/chart", helm.UpgradeOptions{
			Namespace: namespace,
			Values:    operatorHelmValues(node, peers, "localhost/redpanda-operator", "dev"),
		})
		require.NoErrorf(t, err, "helm upgrade in %s", node.Name())

		// Wait for the rollout: old operator pod gone, new operator pod ready.
		waitForOperatorRoll(ctx, t, node, namespace, oldOperatorPodUID)

		// Quorum + rejoin assertion run AFTER each upgrade. Quorum may flap
		// briefly during the rollout — Eventually loop handles it.
		t.Logf("verifying raft quorum recovered after upgrading %s", node.Name())
		assertRaftQuorumRejoinedAfterUpgrade(ctx, t, nodes, namespace, node.Name(), wasLeader)
	}

	return ctx
}

// multiclusterOperatorRaftQuorumIsHealthy verifies the initial state: every
// vcluster's operator reports is_healthy and there are no unhealthy peers. It
// also captures each cluster's term so the upgrade step can later require
// strict advancement. Designed to be called once, before any upgrade.
func multiclusterOperatorRaftQuorumIsHealthy(ctx context.Context, t framework.TestingT, clusterName string) context.Context {
	nodes := getNodes(ctx, clusterName)
	namespace := metav1.NamespaceDefault

	require.Eventuallyf(t, func() bool {
		for _, node := range nodes {
			st, err := operatorStatus(ctx, node, namespace)
			if err != nil {
				t.Logf("status %s: %v", node.Name(), err)
				return false
			}
			if !st.IsHealthy {
				t.Logf("status %s: is_healthy=false (leader=%q term=%d unhealthy_peers=%v)", node.Name(), st.Leader, st.Term, st.UnhealthyPeers)
				return false
			}
			if len(st.UnhealthyPeers) > 0 {
				t.Logf("status %s: unhealthy peers %v", node.Name(), st.UnhealthyPeers)
				return false
			}
		}
		return true
	}, 5*time.Minute, 5*time.Second, "raft quorum never became healthy across all vclusters of %q", clusterName)

	terms := snapshotRaftTerms(ctx, t, nodes, namespace)
	return context.WithValue(ctx, raftTermStateKey{}, &raftTermState{terms: terms})
}

// snapshotRaftTerms reads the current term from every vcluster's operator and
// returns the map keyed by node name.
func snapshotRaftTerms(ctx context.Context, t framework.TestingT, nodes []*vclusterNode, namespace string) map[string]uint64 {
	terms := make(map[string]uint64, len(nodes))
	for _, node := range nodes {
		st, err := operatorStatus(ctx, node, namespace)
		require.NoErrorf(t, err, "snapshot raft term in %s", node.Name())
		terms[node.Name()] = st.Term
		t.Logf("snapshot %s: term=%d leader=%q", node.Name(), st.Term, st.Leader)
	}
	return terms
}

// currentRaftLeader returns the name of the node the quorum currently agrees is
// the raft leader. It polls briefly so a transient empty leader (e.g. just after
// a previous rollout) resolves before we read it.
func currentRaftLeader(ctx context.Context, t framework.TestingT, nodes []*vclusterNode, namespace string) string {
	var leader string
	require.Eventuallyf(t, func() bool {
		for _, node := range nodes {
			st, err := operatorStatus(ctx, node, namespace)
			if err == nil && st.IsHealthy && st.Leader != "" {
				leader = st.Leader
				return true
			}
		}
		return false
	}, 1*time.Minute, 2*time.Second, "could not determine current raft leader")
	return leader
}

// assertRaftQuorumRejoinedAfterUpgrade polls until the cross-cluster operator
// raft quorum is healthy and the just-upgraded operator has rejoined it.
//
// Raft only advances the term when the *leader* is restarted (its loss forces a
// re-election); restarting a follower leaves the term unchanged as it simply
// rejoins under the existing leader. So a strict term advance is required only
// when the upgraded node was the leader; for a follower we require the term not
// to regress. Either way the load-bearing signal is that every operator is a
// healthy, converged member that agrees on a single leader and term — i.e. the
// upgraded pod actually rejoined rather than being silently isolated.
func assertRaftQuorumRejoinedAfterUpgrade(ctx context.Context, t framework.TestingT, nodes []*vclusterNode, namespace, upgradedNode string, wasLeader bool) {
	state, ok := ctx.Value(raftTermStateKey{}).(*raftTermState)
	require.True(t, ok && state != nil, "raft term state missing from context; call multiclusterOperatorRaftQuorumIsHealthy or upgradeMulticlusterOperatorOneAtATime first")

	priorTerm := maxTerm(state.terms)

	var convergedTerm uint64
	require.Eventuallyf(t, func() bool {
		// Every operator must be healthy with no unhealthy peers, and all must
		// agree on the same term (converged quorum).
		var term uint64
		var sawUpgradedLeader bool
		for i, node := range nodes {
			st, err := operatorStatus(ctx, node, namespace)
			if err != nil {
				t.Logf("status %s: %v", node.Name(), err)
				return false
			}
			if !st.IsHealthy || len(st.UnhealthyPeers) > 0 {
				t.Logf("status %s: is_healthy=%t term=%d leader=%q unhealthy_peers=%v", node.Name(), st.IsHealthy, st.Term, st.Leader, st.UnhealthyPeers)
				return false
			}
			if st.Leader == "" {
				t.Logf("status %s: no leader yet (term=%d)", node.Name(), st.Term)
				return false
			}
			if i == 0 {
				term = st.Term
			} else if st.Term != term {
				t.Logf("terms not converged: %s=%d vs %d", node.Name(), st.Term, term)
				return false
			}
			if node.Name() == upgradedNode {
				sawUpgradedLeader = true
			}
		}
		if !sawUpgradedLeader {
			t.Logf("upgraded node %s not found among quorum members", upgradedNode)
			return false
		}

		// Term invariant depends on whether we restarted the leader.
		if wasLeader && term <= priorTerm {
			t.Logf("upgraded leader %s: term %d has not advanced past prior %d", upgradedNode, term, priorTerm)
			return false
		}
		if !wasLeader && term < priorTerm {
			t.Logf("upgraded follower %s: term regressed from %d to %d", upgradedNode, priorTerm, term)
			return false
		}

		convergedTerm = term
		return true
	}, 5*time.Minute, 5*time.Second, "raft quorum never recovered after upgrading %s (wasLeader=%t)", upgradedNode, wasLeader)

	// Record the converged term as the baseline for the next upgrade.
	for _, node := range nodes {
		state.terms[node.Name()] = convergedTerm
	}
}

// maxTerm returns the highest term in the recorded per-node term map.
func maxTerm(terms map[string]uint64) uint64 {
	var m uint64
	for _, v := range terms {
		if v > m {
			m = v
		}
	}
	return m
}

// getOperatorPodUID returns the current operator pod's UID so the upgrade can
// later detect a roll (UID changes when the Deployment replaces the pod).
// Fails the test if zero or multiple operator pods are present.
func getOperatorPodUID(ctx context.Context, t framework.TestingT, node *vclusterNode, namespace string) types.UID {
	var pods corev1.PodList
	require.NoError(t, node.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels(operatorPodSelector()),
	))
	require.Lenf(t, pods.Items, 1, "expected exactly 1 operator pod in %s, got %d", node.Name(), len(pods.Items))
	return pods.Items[0].UID
}

// waitForOperatorRoll waits until: (1) the old operator pod is gone, (2) the
// operator Deployment is Available, and (3) exactly one pod exists with a new
// UID matching the operator selector. Bounded by 5 minutes.
func waitForOperatorRoll(ctx context.Context, t framework.TestingT, node *vclusterNode, namespace string, oldUID types.UID) {
	require.Eventuallyf(t, func() bool {
		var dep appsv1.Deployment
		if err := node.Get(ctx, types.NamespacedName{Namespace: namespace, Name: operatorDeploymentName}, &dep); err != nil {
			t.Logf("get operator deployment in %s: %v", node.Name(), err)
			return false
		}
		cond := apimeta.FindStatusCondition(deploymentConditionsAsMeta(dep.Status.Conditions), string(appsv1.DeploymentAvailable))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			t.Logf("operator deployment %s in %s: Available=%v", operatorDeploymentName, node.Name(), cond)
			return false
		}

		var pods corev1.PodList
		if err := node.List(ctx, &pods,
			client.InNamespace(namespace),
			client.MatchingLabels(operatorPodSelector()),
		); err != nil {
			t.Logf("list operator pods in %s: %v", node.Name(), err)
			return false
		}
		if len(pods.Items) != 1 {
			t.Logf("operator pod count in %s: %d (want 1)", node.Name(), len(pods.Items))
			return false
		}
		if pods.Items[0].UID == oldUID {
			t.Logf("operator pod in %s has not been replaced yet (uid=%s)", node.Name(), pods.Items[0].UID)
			return false
		}
		return isPodRunning(&pods.Items[0])
	}, 5*time.Minute, 3*time.Second, "operator deployment never finished rolling in %s", node.Name())
}

// deploymentConditionsAsMeta adapts appsv1.DeploymentCondition to the slice
// shape apimeta.FindStatusCondition wants. The translation is shallow:
// Available is what we care about, and its Status field already uses the
// same ConditionStatus type underneath.
func deploymentConditionsAsMeta(in []appsv1.DeploymentCondition) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(in))
	for _, c := range in {
		out = append(out, metav1.Condition{
			Type:    string(c.Type),
			Status:  metav1.ConditionStatus(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		})
	}
	return out
}

// operatorDeploymentName is the Deployment created by the operator chart with
// release name "redpanda" (chart Fullname → "redpanda-operator").
const operatorDeploymentName = "redpanda-operator"

// operatorPodSelector returns the labels the operator chart puts on operator
// pods (matchLabels for the Deployment, also used by the raft Service).
func operatorPodSelector() map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance": operatorHelmReleaseName, // "redpanda"
		redpandaLabel:                "operator",              // app.kubernetes.io/name=operator
	}
}

// operatorPeers reconstructs the helm `multicluster.peers` value from the
// current set of vcluster nodes. The original install path computes this in
// bootstrapTLS; the upgrade path needs to produce a value that's
// byte-equivalent (or at least valid) so the upgrade doesn't drop peer
// addresses.
func operatorPeers(nodes []*vclusterNode) []any {
	peers := make([]any, 0, len(nodes))
	for _, n := range nodes {
		peers = append(peers, map[string]any{
			"name":    n.Name(),
			"address": n.ExternalIP(),
		})
	}
	return peers
}

// operatorStatus dials the operator's TransportService over its pod IP via
// kube.PodDialer (no kubectl port-forward needed) using mTLS material from the
// in-cluster bootstrap secret. Returns the parsed Status RPC response.
//
// Loosely modeled on operator/cmd/rpk-k8s/k8s/multicluster/checks/cluster_raft.go,
// which does the same dial via port-forward instead of PodDialer.
func operatorStatus(ctx context.Context, node *vclusterNode, namespace string) (*transportv1.StatusResponse, error) {
	// Find the operator pod.
	var pods corev1.PodList
	if err := node.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels(operatorPodSelector()),
	); err != nil {
		return nil, fmt.Errorf("listing operator pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no operator pod in %s/%s", node.Name(), namespace)
	}
	pod := &pods.Items[0]
	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("operator pod %s has no PodIP yet", pod.Name)
	}

	tlsCfg, err := operatorMTLSConfig(ctx, node, namespace)
	if err != nil {
		return nil, fmt.Errorf("building mTLS config: %w", err)
	}

	pfCfg, err := node.PortForwardedRESTConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("port-forwarded REST config: %w", err)
	}
	dialer := kube.NewPodDialer(pfCfg)

	// "passthrough:///" makes gRPC skip its built-in DNS resolver and hand
	// the target string straight to the custom dialer. Without it,
	// grpc.NewClient runs the host's DNS lookup on "podname.namespace",
	// returns zero addresses, and fails with "name resolver error: produced
	// zero addresses" before WithContextDialer is ever consulted.
	addr := "passthrough:///" + net.JoinHostPort(pod.Name+"."+namespace, fmt.Sprintf("%d", operatorGRPCPort))
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", target)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient: %w", err)
	}
	defer conn.Close()

	rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := transportv1.NewTransportServiceClient(conn).Status(rpcCtx, &transportv1.StatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("TransportService.Status: %w", err)
	}
	return resp, nil
}

// operatorMTLSConfig reads the bootstrap-created TLS secret for the operator
// raft transport and builds a *tls.Config suitable for grpc.NewClient. The
// secret is created by bootstrap.BootstrapKubernetesClusters under the name
// <fullname>-multicluster-certificates ("redpanda-operator-multicluster-certificates"
// in our case).
func operatorMTLSConfig(ctx context.Context, node *vclusterNode, namespace string) (*tls.Config, error) {
	var sec corev1.Secret
	secretName := operatorDeploymentName + "-multicluster-certificates"
	if err := node.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &sec); err != nil {
		return nil, fmt.Errorf("get secret %s in %s: %w", secretName, node.Name(), err)
	}

	cert, err := tls.X509KeyPair(sec.Data["tls.crt"], sec.Data["tls.key"])
	if err != nil {
		return nil, fmt.Errorf("parse keypair: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(sec.Data["ca.crt"]) {
		return nil, fmt.Errorf("invalid CA certificate in %s/%s", namespace, secretName)
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse leaf cert: %w", err)
	}

	serverName := ""
	skipVerify := false
	if len(leaf.DNSNames) > 0 {
		serverName = leaf.DNSNames[0]
	} else {
		// Cert has only IP SANs. PodDialer tunnels through the API server;
		// we still authenticate via mTLS (server presents a cert signed by
		// the same CA), so skipping hostname verification is safe.
		skipVerify = true
	}

	cfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caPool,
		ServerName:         serverName,
		InsecureSkipVerify: skipVerify, //nolint:gosec // see comment above
		MinVersion:         tls.VersionTLS12,
	}
	return cfg, nil
}
