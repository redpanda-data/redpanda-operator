// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultAdminPort = 9644
	adminPortName    = "admin"
)

// BrokerHealthCheck port-forwards to a running broker pod and queries the
// Redpanda admin API for cluster health, broker list, and configuration
// status. Only runs on the first cluster that has running pods (since the
// admin API reflects the state of the entire Redpanda cluster regardless of
// which broker is queried).
//
// Requires ResourceCheck and SecretsCheck to have run.
type BrokerHealthCheck struct{}

func (c *BrokerHealthCheck) Name() string { return "broker-health" }

func (c *BrokerHealthCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	if cc.StretchCluster == nil {
		return []Result{Skip(c.Name(), "StretchCluster not available")}
	}

	// Find a running broker pod.
	pod, adminPort, err := findBrokerPod(ctx, cc)
	if err != nil {
		return []Result{Skip(c.Name(), fmt.Sprintf("no running broker pod found: %v", err))}
	}

	// Port-forward to the pod's admin API.
	forwardedPorts, stop, err := cc.Ctl.PortForward(ctx, pod, io.Discard, io.Discard)
	if err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("port-forward to %s failed: %v", pod.Name, err))}
	}
	defer stop()

	var localPort uint16
	for _, fp := range forwardedPorts {
		if fp.Remote == adminPort {
			localPort = fp.Local
			break
		}
	}
	if localPort == 0 {
		return []Result{Fail(c.Name(), fmt.Sprintf("admin port %d not found in forwarded ports for pod %s", adminPort, pod.Name))}
	}

	// Build admin API client.
	admin, err := buildAdminClient(cc, localPort)
	if err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("admin API connection failed: %v", err))}
	}
	defer admin.Close()
	cc.AdminConnected = true

	var results []Result

	// Health overview.
	health, err := admin.GetHealthOverview(ctx)
	if err != nil {
		results = append(results, Fail(c.Name(), fmt.Sprintf("health check failed: %v", err)))
	} else {
		cc.Health = &health
		if health.IsHealthy {
			results = append(results, Pass(c.Name(),
				fmt.Sprintf("cluster healthy (%d broker(s), %d down)", len(health.AllNodes), len(health.NodesDown))))
		} else {
			msg := fmt.Sprintf("cluster unhealthy (%d broker(s), %d down", len(health.AllNodes), len(health.NodesDown))
			if len(health.LeaderlessPartitions) > 0 {
				msg += fmt.Sprintf(", %d leaderless partition(s)", len(health.LeaderlessPartitions))
			}
			if len(health.UnderReplicatedPartitions) > 0 {
				msg += fmt.Sprintf(", %d under-replicated partition(s)", len(health.UnderReplicatedPartitions))
			}
			msg += ")"
			results = append(results, Fail(c.Name(), msg))
		}
	}

	// Broker list.
	if cc.Health != nil {
		for _, brokerID := range cc.Health.AllNodes {
			broker, err := admin.Broker(ctx, brokerID)
			if err != nil {
				results = append(results, Fail(c.Name(), fmt.Sprintf("cannot fetch broker %d: %v", brokerID, err)))
				continue
			}
			cc.Brokers = append(cc.Brokers, broker)
		}

		// Check for decommissioning brokers.
		for _, broker := range cc.Brokers {
			if broker.MembershipStatus == rpadmin.MembershipStatusDraining {
				status, err := admin.DecommissionBrokerStatus(ctx, broker.NodeID)
				if err != nil {
					results = append(results, Fail(c.Name(),
						fmt.Sprintf("broker %d is decommissioning (cannot query progress: %v)", broker.NodeID, err)))
				} else if !status.Finished {
					results = append(results, Fail(c.Name(),
						fmt.Sprintf("broker %d is decommissioning (%d partition(s) remaining)",
							broker.NodeID, status.ReplicasLeft)))
				}
			}
		}
	}

	// Config status.
	configStatus, err := admin.ClusterConfigStatus(ctx, true)
	if err != nil {
		results = append(results, Fail(c.Name(), fmt.Sprintf("config status check failed: %v", err)))
	} else {
		cc.ConfigStatus = configStatus

		var invalid, unknown []string
		needsRestart := false
		for _, s := range configStatus {
			invalid = append(invalid, s.Invalid...)
			unknown = append(unknown, s.Unknown...)
			needsRestart = needsRestart || s.Restart
		}

		if len(invalid) > 0 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("invalid config properties: %s", strings.Join(invalid, ", "))))
		}
		if len(unknown) > 0 {
			results = append(results, Fail(c.Name(),
				fmt.Sprintf("unknown config properties: %s", strings.Join(unknown, ", "))))
		}
		if needsRestart {
			results = append(results, Fail(c.Name(), "one or more brokers need a restart to apply configuration changes"))
		}
		if len(invalid) == 0 && len(unknown) == 0 && !needsRestart {
			results = append(results, Pass(c.Name(), "no configuration issues"))
		}
	}

	return results
}

// findBrokerPod finds a running pod owned by the StretchCluster.
func findBrokerPod(ctx context.Context, cc *CheckContext) (*corev1.Pod, uint16, error) {
	var pods corev1.PodList
	if err := cc.Ctl.List(ctx, cc.Namespace, &pods, client.MatchingLabels{
		ownerLabel: cc.StretchCluster.Name,
	}); err != nil {
		return nil, 0, fmt.Errorf("listing pods: %w", err)
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		port := adminPortFromPod(pod)
		if port > 0 {
			return pod, port, nil
		}
	}

	return nil, 0, fmt.Errorf("no running broker pod with admin port found in namespace %s", cc.Namespace)
}

// adminPortFromPod finds the admin API container port on a pod.
func adminPortFromPod(pod *corev1.Pod) uint16 {
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.Name == adminPortName {
				return uint16(port.ContainerPort)
			}
		}
	}
	return defaultAdminPort
}

// buildAdminClient creates an rpadmin.AdminAPI client pointed at a port-forwarded
// local address. It reads TLS and auth credentials from the CheckContext.
func buildAdminClient(cc *CheckContext, localPort uint16) (*rpadmin.AdminAPI, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", localPort)

	spec := cc.StretchCluster.Spec.DeepCopy()
	spec.MergeDefaults()

	tlsEnabled := false
	if spec.Listeners != nil && spec.Listeners.Admin != nil {
		tlsEnabled = spec.Listeners.Admin.IsTLSEnabled(spec.TLS)
	}

	var tlsConfig *tls.Config
	if tlsEnabled {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Port-forwarding goes to localhost — the cert's SAN won't match,
			// so we skip server name verification but still load the CA pool
			// for chain validation where possible.
			InsecureSkipVerify: true, //nolint:gosec
		}
		for _, caSecret := range cc.CASecrets {
			caCert, ok := caSecret.Data["ca.crt"]
			if !ok {
				continue
			}
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = pool
				break
			}
		}
	}

	var auth rpadmin.Auth = &rpadmin.NopAuth{}
	if cc.BootstrapSecret != nil {
		pw, ok := cc.BootstrapSecret.Data[bootstrapUserPasswordKey]
		if ok && len(pw) > 0 {
			auth = &rpadmin.BasicAuth{
				Username: "kubernetes-controller",
				Password: string(pw),
			}
		}
	}

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	urls := []string{fmt.Sprintf("%s://%s", scheme, addr)}

	return rpadmin.NewAdminAPI(urls, auth, tlsConfig)
}
