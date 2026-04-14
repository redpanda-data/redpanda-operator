// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	sigs_client "sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// bootstrapNode is a single-replica raft node running in bootstrap mode with a
// custom kubeconfig fetcher backed by envtest.
type bootstrapNode struct {
	name    string
	mgr     multicluster.Manager
	tracker *engageTracker
	cancel  context.CancelFunc
	done    chan error
	stopped bool
	// client is a k8s client for the local envtest cluster, used to read
	// secrets written by the startup fetcher.
	client sigs_client.Client
}

// stop cancels the node's context and waits for it to shut down.
func (n *bootstrapNode) stop(t *testing.T) {
	t.Helper()
	n.cancel()
	select {
	case <-n.done:
	case <-time.After(stopTimeout):
		t.Fatalf("bootstrap node %s did not stop in time", n.name)
	}
	n.stopped = true
}

// restConfigToKubeconfigBytes serialises a REST config into kubeconfig YAML
// bytes that loadKubeconfigFromBytes can later parse back into a *rest.Config.
// Envtest uses client-cert auth, so we embed the cert/key/CA directly.
func restConfigToKubeconfigBytes(name string, cfg *rest.Config) ([]byte, error) {
	kc := clientcmdapi.NewConfig()
	kc.Clusters[name] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	kc.AuthInfos[name] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
	}
	kc.Contexts[name] = &clientcmdapi.Context{
		Cluster:  name,
		AuthInfo: name,
	}
	kc.CurrentContext = name
	return clientcmd.Write(*kc)
}

// kubeconfigSecretName mirrors the package-internal naming used for cache
// secrets: <kubeconfigName>-<peerName>.
func kubeconfigSecretName(kubeconfigName, peerName string) string {
	return kubeconfigName + "-" + peerName
}

// setupBootstrapNodes creates numNodes single-replica envtest instances all
// wired together via raft in bootstrap mode. Each node uses a custom
// KubeconfigFetcher that returns its envtest REST config directly (no
// ServiceAccount/token dance needed in tests). An engageTracker runnable is
// registered on every node so that we can observe cluster engagement after
// leader election.
func setupBootstrapNodes(t *testing.T, ctx context.Context, numNodes int) []*bootstrapNode {
	t.Helper()

	logger := testr.New(t)
	ctrllog.SetLogger(logger)

	const (
		kubeconfigName      = "test-kubeconfig"
		kubeconfigNamespace = "default"
	)

	ports := testutil.FreePorts(t, numNodes)
	peers := make([]multicluster.RaftCluster, numNodes)
	for i := range numNodes {
		peers[i] = multicluster.RaftCluster{
			Name:    fmt.Sprintf("cluster-%d", i),
			Address: fmt.Sprintf("127.0.0.1:%d", ports[i]),
		}
	}

	nodes := make([]*bootstrapNode, numNodes)
	for i := range numNodes {
		env := kubetest.NewEnv(t)
		clusterName := peers[i].Name
		restCfg := env.RestConfig()

		// The fetcher for this node just serialises its own envtest REST
		// config. This avoids the SA/token setup that CreateRemoteKubeconfig
		// normally performs, keeping the test self-contained.
		fetcher := leaderelection.KubeconfigFetcherFn(func(ctx context.Context) ([]byte, error) {
			return restConfigToKubeconfigBytes(clusterName, restCfg)
		})

		cl, err := sigs_client.New(restCfg, sigs_client.Options{Scheme: env.Scheme()})
		require.NoError(t, err)

		rctx, rcancel := context.WithCancel(ctx)
		mgr, err := multicluster.NewRaftRuntimeManager(&multicluster.RaftConfiguration{
			Name:                clusterName,
			Address:             peers[i].Address,
			Peers:               peers,
			RestConfig:          restCfg,
			Scheme:              env.Scheme(),
			Logger:              logger.WithName(clusterName),
			Insecure:            true,
			SkipNameValidation:  true,
			Bootstrap:           true,
			Fetcher:             fetcher,
			KubeconfigName:      kubeconfigName,
			KubeconfigNamespace: kubeconfigNamespace,
			ElectionTimeout:     raftElectionTimeout,
			HeartbeatInterval:   raftHeartbeatInterval,
			GRPCMaxBackoff:      raftGRPCMaxBackoff,
		})
		require.NoError(t, err)

		tracker := newEngageTracker()
		require.NoError(t, mgr.Add(tracker))

		done := make(chan error, 1)
		go func() {
			done <- mgr.Start(rctx)
			close(done)
		}()

		nodes[i] = &bootstrapNode{
			name:    clusterName,
			mgr:     mgr,
			tracker: tracker,
			cancel:  rcancel,
			done:    done,
			client:  cl,
		}
	}

	t.Cleanup(func() {
		for _, n := range nodes {
			n.cancel()
		}
		for _, n := range nodes {
			<-n.done
		}
	})

	return nodes
}

// findBootstrapLeader returns the node whose name equals the current raft
// leader, or nil if no leader has been elected yet. Stopped nodes are skipped
// both as reporters and as candidates — a stopped node's manager retains a
// stale currentLeader atomic and must not be returned as the new leader.
func findBootstrapLeader(nodes []*bootstrapNode) *bootstrapNode {
	for _, n := range nodes {
		if n.stopped {
			continue
		}
		leader := n.mgr.GetLeader()
		if leader == "" {
			continue
		}
		for _, m := range nodes {
			if m.stopped {
				continue
			}
			if m.name == leader {
				return m
			}
		}
	}
	return nil
}

// TestIntegrationKubeconfigCaching verifies two properties of the kubeconfig
// cache introduced to make raft-leader failover resilient:
//
//  1. Startup caching: every node's startupKubeconfigFetcher fetches each
//     peer's kubeconfig via gRPC and stores it as a Secret in the local
//     cluster, so that a future raft leader on that node can read from the
//     cache without a live gRPC call.
//
//  2. Cache-first failover: when the raft leader crashes (its gRPC transport
//     is shut down), a newly elected leader can still engage the crashed
//     cluster by reading from its local cache — without any gRPC connectivity
//     to the former leader.
//
// Setup: 3 envtest instances, 1 replica each, bootstrap mode with a custom
// fetcher that returns the envtest REST config directly.
func TestIntegrationKubeconfigCaching(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx := testutil.Context(t)
	nodes := setupBootstrapNodes(t, ctx, 3)

	const (
		kubeconfigName      = "test-kubeconfig"
		kubeconfigNamespace = "default"
	)

	// ── Phase 1: wait for initial raft leader election ───────────────────────

	t.Log("waiting for initial raft leader election...")
	var leaderNode *bootstrapNode
	require.Eventually(t, func() bool {
		leaderNode = findBootstrapLeader(nodes)
		return leaderNode != nil
	}, waitTimeout, waitInterval, "raft leader should be elected")
	t.Logf("initial raft leader: %s", leaderNode.name)

	// ── Phase 2: verify startup caching ──────────────────────────────────────
	//
	// Every node's startupKubeconfigFetcher should have written a Secret for
	// each of its peers. We check all 3 × 2 = 6 expected secrets.

	t.Log("waiting for startup kubeconfig cache to be populated...")
	for _, node := range nodes {
		for _, peer := range nodes {
			if peer.name == node.name {
				continue
			}
			secretName := kubeconfigSecretName(kubeconfigName, peer.name)
			require.Eventually(t, func() bool {
				var secret corev1.Secret
				err := node.client.Get(ctx, types.NamespacedName{
					Name:      secretName,
					Namespace: kubeconfigNamespace,
				}, &secret)
				if err != nil {
					return false
				}
				return len(secret.Data["kubeconfig.yaml"]) > 0
			}, waitTimeout, waitInterval,
				"node %s should have cached kubeconfig secret %s for peer %s",
				node.name, secretName, peer.name)
			t.Logf("node %s: kubeconfig secret %s/%s is present", node.name, kubeconfigNamespace, secretName)
		}
	}

	// ── Phase 3: kill the raft leader ────────────────────────────────────────
	//
	// Stopping the leader node shuts down its gRPC transport. Any subsequent
	// gRPC Kubeconfig call to that node will fail. A new raft leader must
	// therefore rely on the locally cached Secret to engage the former
	// leader's cluster.

	t.Logf("stopping raft leader %s...", leaderNode.name)
	leaderNode.stop(t)
	t.Log("leader stopped")

	// ── Phase 4: wait for new raft leader ────────────────────────────────────

	t.Log("waiting for new raft leader election...")
	var newLeaderNode *bootstrapNode
	require.Eventually(t, func() bool {
		newLeaderNode = findBootstrapLeader(nodes)
		// Must be a different node than the one we stopped.
		return newLeaderNode != nil && newLeaderNode.name != leaderNode.name
	}, waitTimeout, waitInterval, "a new raft leader should be elected after the old leader stops")
	t.Logf("new raft leader: %s", newLeaderNode.name)

	// ── Phase 5: verify the new leader engaged the former leader's cluster ───
	//
	// The new leader's bootstrap routine reads the former leader's kubeconfig
	// from its local Secret cache (written in phase 2). It creates a
	// cluster.Cluster and calls AddOrReplace, which triggers doEngage on the
	// registered engageTracker. If the cache was NOT used, the gRPC call
	// would fail permanently (former leader's gRPC is down) and Engage would
	// never be called.

	t.Logf("verifying new leader %s engaged former leader's cluster %s via cache...", newLeaderNode.name, leaderNode.name)
	require.Eventually(t, func() bool {
		return newLeaderNode.tracker.engageCount(leaderNode.name) > 0
	}, waitTimeout, waitInterval,
		"new leader %s should have engaged former leader's cluster %s using the cached kubeconfig",
		newLeaderNode.name, leaderNode.name)

	t.Logf("new leader %s successfully engaged %s via cached kubeconfig", newLeaderNode.name, leaderNode.name)
}
