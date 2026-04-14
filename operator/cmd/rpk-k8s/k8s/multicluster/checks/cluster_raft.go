// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	corev1 "k8s.io/api/core/v1"

	transportv1 "github.com/redpanda-data/redpanda-operator/pkg/multicluster/leaderelection/proto/gen/transport/v1"
)

const raftPort = 9443

// RaftCheck port-forwards to the operator pod's gRPC transport and queries the
// Status RPC to obtain raft state, leader, term, peers, and health.
// Requires cc.Pod to be set (by PodCheck). Populates cc.RaftStatus.
type RaftCheck struct{}

func (c *RaftCheck) Name() string { return "raft" }

func (c *RaftCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	if cc.Pod == nil {
		return []Result{Fail(c.Name(), "skipped: no operator pod found")}
	}
	if cc.Pod.Status.Phase != corev1.PodRunning {
		return []Result{Fail(c.Name(), fmt.Sprintf("skipped: pod %s is not running (phase: %s)", cc.Pod.Name, cc.Pod.Status.Phase))}
	}

	status, err := queryRaftStatus(ctx, cc.Ctl, cc.Namespace, cc.SecretPrefix, cc.Pod)
	if err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("cannot query raft status: %v", err))}
	}
	cc.RaftStatus = status

	var results []Result
	if !status.IsHealthy {
		results = append(results, Fail(c.Name(), "raft cluster reports unhealthy (no leader)"))
	} else {
		results = append(results, Pass(c.Name(), fmt.Sprintf("raft healthy: state=%s leader=%s term=%d peers=%d",
			status.RaftState, status.Leader, status.Term, len(status.ClusterNames))))
	}
	if len(status.UnhealthyPeers) > 0 {
		results = append(results, Fail(c.Name(), fmt.Sprintf("unhealthy peers: %s", strings.Join(status.UnhealthyPeers, ", "))))
	}
	return results
}

func queryRaftStatus(ctx context.Context, ctl *kube.Ctl, namespace, secretPrefix string, pod *corev1.Pod) (*transportv1.StatusResponse, error) {
	forwardedPorts, stop, err := ctl.PortForward(ctx, pod, io.Discard, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("port-forward to %s: %w", pod.Name, err)
	}
	defer stop()

	// Find the forwarded local port that maps to the raft gRPC port.
	var localPort uint16
	for _, fp := range forwardedPorts {
		if fp.Remote == raftPort {
			localPort = fp.Local
			break
		}
	}
	if localPort == 0 {
		return nil, fmt.Errorf("port %d not found in forwarded ports for pod %s", raftPort, pod.Name)
	}

	tlsCreds, serverName, err := grpcCredsFromPod(ctx, ctl, namespace, secretPrefix, pod)
	if err != nil {
		return nil, fmt.Errorf("building gRPC credentials: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", localPort)

	tlsConfig := &tls.Config{} //nolint:gosec
	tlsCreds(tlsConfig)
	tlsConfig.ServerName = serverName

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
	if err != nil {
		return nil, fmt.Errorf("gRPC dial: %w", err)
	}
	defer conn.Close()

	client := transportv1.NewTransportServiceClient(conn)

	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return client.Status(callCtx, &transportv1.StatusRequest{})
}

func grpcCredsFromPod(ctx context.Context, ctl *kube.Ctl, namespace, secretPrefix string, pod *corev1.Pod) (func(*tls.Config), string, error) {
	var secretName string
	// Prefer exact match: look for the volume whose name is
	// <secretPrefix>-multicluster-certificates and read SecretName from it.
	if secretPrefix != "" {
		target := secretPrefix + "-multicluster-certificates"
		for _, vol := range pod.Spec.Volumes {
			if vol.Name == target && vol.Secret != nil {
				secretName = vol.Secret.SecretName
				break
			}
		}
	}
	// Fallback: any volume whose name ends with the multicluster suffix.
	if secretName == "" {
		for _, vol := range pod.Spec.Volumes {
			if strings.HasSuffix(vol.Name, "-multicluster-certificates") && vol.Secret != nil {
				secretName = vol.Secret.SecretName
				break
			}
		}
	}
	if secretName == "" {
		return nil, "", fmt.Errorf("pod %s has no multicluster-certificates volume", pod.Name)
	}

	var secret corev1.Secret
	if err := ctl.Get(ctx, kube.ObjectKey{Name: secretName, Namespace: namespace}, &secret); err != nil {
		return nil, "", fmt.Errorf("getting secret %s: %w", secretName, err)
	}

	certificate, err := tls.X509KeyPair(secret.Data["tls.crt"], secret.Data["tls.key"])
	if err != nil {
		return nil, "", fmt.Errorf("loading key pair: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(secret.Data["ca.crt"]) {
		return nil, "", fmt.Errorf("invalid CA certificate")
	}

	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, "", fmt.Errorf("parsing leaf certificate: %w", err)
	}

	// Determine the ServerName for TLS verification. The cert may have DNS
	// SANs (service FQDN) or only IP SANs (ClusterIP). When port-forwarding
	// we connect to 127.0.0.1, so we need to set ServerName to a SAN that
	// the cert actually contains. If the cert only has IP SANs (no DNS
	// names), we skip hostname verification since we're already
	// authenticating via mTLS and the connection goes through a
	// port-forward tunnel.
	serverName := ""
	skipVerify := false
	if len(leaf.DNSNames) > 0 {
		serverName = leaf.DNSNames[0]
	} else {
		skipVerify = true
	}

	return func(cfg *tls.Config) {
		cfg.Certificates = []tls.Certificate{certificate}
		cfg.RootCAs = caPool
		if skipVerify {
			cfg.InsecureSkipVerify = true //nolint:gosec // port-forward tunnel; mTLS authenticates the peer
		}
	}, serverName, nil
}
