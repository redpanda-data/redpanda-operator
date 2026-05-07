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
	"path"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// metricsFetcher abstracts the apiserver-proxy GET against /metrics. The
// production implementation goes through kubernetes.Interface; tests stub it
// because envtest doesn't run kubelets and there is no real metrics server
// to scrape.
type metricsFetcher interface {
	// Metrics returns the raw response body of GET /metrics on the named
	// pod's container port. scheme is "http" or "https" depending on
	// whether the metrics server is TLS-terminated (the apiserver proxy
	// uses the prefix "<scheme>:<pod>:<port>" to pick).
	Metrics(ctx context.Context, namespace, podName, scheme string, port int) ([]byte, error)
}

// kubeMetricsFetcher implements metricsFetcher via the apiserver pod-proxy
// subresource. This works for both plain HTTP and TLS-terminated metrics
// servers because the apiserver handles the TLS handshake on our behalf;
// the bundle command only needs RBAC to "pods/proxy" in the operator's
// namespace.
type kubeMetricsFetcher struct {
	cs kubernetes.Interface
}

func newKubeMetricsFetcher(cfg *rest.Config) (*kubeMetricsFetcher, error) {
	if cfg == nil {
		return nil, fmt.Errorf("newKubeMetricsFetcher: nil rest.Config")
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes clientset: %w", err)
	}
	return &kubeMetricsFetcher{cs: cs}, nil
}

func (k *kubeMetricsFetcher) Metrics(ctx context.Context, namespace, podName, scheme string, port int) ([]byte, error) {
	// The apiserver pod-proxy expects the resource name as
	// "<scheme>:<pod>:<port>" for HTTPS and either "<pod>:<port>" or
	// "http:<pod>:<port>" for HTTP. We pass the scheme explicitly to keep
	// the call site obvious.
	name := fmt.Sprintf("%s:%s:%d", scheme, podName, port)
	if scheme == "" {
		name = fmt.Sprintf("%s:%d", podName, port)
	}
	return k.cs.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		Name(name).
		SubResource("proxy").
		Suffix("metrics").
		DoRaw(ctx)
}

// collectClusterMetrics scrapes the operator's /metrics endpoint via the
// apiserver pod-proxy and writes the raw Prometheus exposition into the
// bundle at clusters/<context>/metrics/metrics.txt.
//
// Returns nil (no work, no error) when:
//
//   - cc.Pod is nil (PodCheck didn't find a pod)
//   - the operator deployment doesn't expose --metrics-bind-address
//     (metrics server disabled)
//   - the deploy args couldn't be parsed for a port
//
// Per-cluster scrape failures are returned as []error so the bundle can
// continue and record them in errors.txt.
func collectClusterMetrics(ctx context.Context, bw *bundleWriter, cc *checks.CheckContext, fetcher metricsFetcher) []error {
	if cc == nil || cc.Pod == nil || fetcher == nil {
		return nil
	}

	port, ok := parseMetricsPort(cc.DeployArgs)
	if !ok {
		// The operator was deployed without --metrics-bind-address, so
		// there's no metrics server to scrape. Not an error — skip.
		return nil
	}
	scheme := metricsScheme(cc.DeployArgs)

	data, err := fetcher.Metrics(ctx, cc.Namespace, cc.Pod.Name, scheme, port)
	if err != nil {
		return []error{fmt.Errorf("scraping metrics from %s/%s (%s://:%d): %w",
			cc.Pod.Namespace, cc.Pod.Name, scheme, port, err)}
	}
	if werr := bw.writeBytes(path.Join("clusters", cc.Context, "metrics", "metrics.txt"), data); werr != nil {
		return []error{werr}
	}
	return nil
}

// parseMetricsPort returns the port the operator's metrics server listens
// on, parsed from --metrics-bind-address in the deployment's container
// args. The flag takes a "[host]:port" value; we ignore the host (which is
// typically empty meaning "listen on all interfaces").
//
// Returns ok=false when:
//
//   - the flag isn't present (metrics disabled)
//   - the flag value is empty (metrics disabled — controller-runtime
//     treats "" as disabled)
//   - the flag value doesn't parse as a valid port number
func parseMetricsPort(args []string) (int, bool) {
	const flag = "--metrics-bind-address"
	value := checks.ExtractFlag(args, flag)
	if value == "" {
		return 0, false
	}
	// Strip the leading host (default ":8443" → "8443"). net.SplitHostPort
	// is overkill here and rejects bare ports, so do the simple thing.
	if i := strings.LastIndex(value, ":"); i >= 0 {
		value = value[i+1:]
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// metricsScheme returns "https" when the deployment was started with
// --metrics-cert-path / --metrics-key-path (controller-runtime turns on
// SecureServing in that case), and "http" otherwise. Used by the
// apiserver pod-proxy URL builder.
func metricsScheme(args []string) string {
	if checks.ExtractFlag(args, "--metrics-cert-path") != "" {
		return "https"
	}
	return "http"
}
