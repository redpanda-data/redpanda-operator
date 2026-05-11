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
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// MetricsOptions controls how often /metrics is sampled per cluster. Two
// samples (the default) at a 10s interval matches `rpk debug bundle`'s
// behaviour and lets investigators compute counter rates post-hoc without a
// live Prometheus.
type MetricsOptions struct {
	Samples  int
	Interval time.Duration
}

const (
	// defaultMetricsSamples is the default number of /metrics scrape
	// samples per cluster. Mirrors rpk debug bundle's default of 2 — the
	// minimum that lets you compute counter rate-of-change.
	defaultMetricsSamples = 2
	// defaultMetricsInterval is the default wall-clock interval between
	// successive /metrics samples. Mirrors rpk debug bundle's default.
	defaultMetricsInterval = 10 * time.Second
)

// metricsFetcher abstracts a GET against the operator's /metrics endpoint.
// The production implementation port-forwards into the pod and authenticates
// with a Bearer token minted via the TokenRequest API; tests stub it because
// envtest doesn't run kubelets and there is no real metrics server to scrape.
type metricsFetcher interface {
	// Metrics returns the raw response body of GET /metrics on the named
	// pod's container port. scheme is "http" or "https" depending on
	// whether the metrics server is TLS-terminated.
	Metrics(ctx context.Context, namespace, podName, scheme string, port int) ([]byte, error)
}

// kubeMetricsFetcher implements metricsFetcher by port-forwarding to the
// operator pod's metrics port and scraping /metrics with a Bearer token
// minted via the TokenRequest API for the pod's ServiceAccount.
//
// Why not the apiserver pod-proxy: the operator wires its metrics server up
// with controller-runtime's filters.WithAuthenticationAndAuthorization,
// which expects either a Bearer token or a client cert that the metrics
// server's authenticator can validate. The apiserver pod-proxy opens a new
// HTTP connection to the pod and does NOT propagate the user's auth
// headers — the metrics server sees an unauthenticated request and returns
// 401, surfaced to client-go as "the server has asked for the client to
// provide credentials". Port-forward sidesteps that: we present the SA's
// minted token directly to the metrics filter, which TokenReview's it
// against the apiserver and forwards the identity into the
// SubjectAccessReview for /metrics.
//
// The scrape will still 403 if the SA doesn't have nonResourceURLs:
// /metrics granted (the chart doesn't grant this by default — separate
// follow-up). The error in that case is recorded in errors.txt and the
// rest of the bundle still completes.
type kubeMetricsFetcher struct {
	ctl *kube.Ctl
	cs  kubernetes.Interface
}

func newKubeMetricsFetcher(ctl *kube.Ctl) (*kubeMetricsFetcher, error) {
	if ctl == nil {
		return nil, fmt.Errorf("newKubeMetricsFetcher: nil kube.Ctl")
	}
	cs, err := kubernetes.NewForConfig(ctl.RestConfig())
	if err != nil {
		return nil, fmt.Errorf("building kubernetes clientset: %w", err)
	}
	return &kubeMetricsFetcher{ctl: ctl, cs: cs}, nil
}

// Metrics opens a port-forward to the metrics port on `podName`, mints a
// Bearer token for the pod's ServiceAccount via the TokenRequest API, and
// GETs /metrics over the forwarded port with that token.
func (k *kubeMetricsFetcher) Metrics(ctx context.Context, namespace, podName, scheme string, port int) ([]byte, error) {
	pod, err := k.cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting pod %s/%s: %w", namespace, podName, err)
	}

	forwarded, stop, err := k.ctl.PortForward(ctx, pod, io.Discard, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("port-forwarding to %s/%s: %w", namespace, podName, err)
	}
	defer stop()

	localPort, ok := pickForwardedPort(forwarded, uint16(port))
	if !ok {
		return nil, fmt.Errorf("metrics port %d not declared as a containerPort on pod %s/%s — port-forward returned ports %v",
			port, namespace, podName, forwardedSummary(forwarded))
	}

	saName := pod.Spec.ServiceAccountName
	if saName == "" {
		saName = "default"
	}
	tokenResp, err := k.cs.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, saName,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: ptr.To[int64](600),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("requesting Bearer token for ServiceAccount %s/%s: %w", namespace, saName, err)
	}

	return scrapeMetrics(ctx, scheme, localPort, tokenResp.Status.Token)
}

// pickForwardedPort returns the local port that PortForward mapped to the
// given remote port, or false if the remote port wasn't forwarded.
func pickForwardedPort(forwarded []portforward.ForwardedPort, remote uint16) (uint16, bool) {
	for _, fp := range forwarded {
		if fp.Remote == remote {
			return fp.Local, true
		}
	}
	return 0, false
}

// forwardedSummary renders a port-forward result as a compact debug string
// for use in error messages.
func forwardedSummary(forwarded []portforward.ForwardedPort) []string {
	out := make([]string, 0, len(forwarded))
	for _, fp := range forwarded {
		out = append(out, fmt.Sprintf("%d->%d", fp.Local, fp.Remote))
	}
	return out
}

// scrapeMetrics performs the HTTP(S) GET /metrics with a Bearer token. The
// scheme is "http" or "https" — for HTTPS we accept any server cert
// because the metrics server typically uses self-signed certs and we're
// connecting via 127.0.0.1 anyway.
func scrapeMetrics(ctx context.Context, scheme string, localPort uint16, token string) ([]byte, error) {
	url := fmt.Sprintf("%s://127.0.0.1:%d/metrics", scheme, localPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building metrics request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			// Self-signed by default — and we're hitting 127.0.0.1
			// over a port-forward, so MITM risk is moot.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraping %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s body: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scraping %s: HTTP %d %s — body: %s",
			url, resp.StatusCode, resp.Status, truncate(body, 512))
	}
	return body, nil
}

// truncate trims a byte slice to n bytes for use in error messages.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}

// collectClusterMetrics scrapes the operator's /metrics endpoint
// opts.Samples times at opts.Interval and writes each Prometheus exposition
// into the bundle at clusters/<context>/metrics/t<i>_metrics.txt. Multiple
// samples let investigators compute counter rate-of-change post-hoc without
// needing a live Prometheus — matches `rpk debug bundle`'s behaviour.
// The actual transport is an implementation detail of the metricsFetcher;
// production goes through port-forward + Bearer-token auth (see
// kubeMetricsFetcher).
//
// Returns nil (no work, no error) when:
//
//   - cc.Pod is nil (PodCheck didn't find a pod)
//   - the operator deployment doesn't expose --metrics-bind-address
//     (metrics server disabled)
//   - the deploy args couldn't be parsed for a port
//
// Per-sample scrape failures are recorded in the returned []error and the
// next sample is still attempted. Context cancellation between samples
// returns immediately with whatever has been collected so far.
//
// progress, when non-nil, is called once per sample with a stderr-style
// "[<context>] sample i/N" message. Run wires this to the --verbose
// progress logger.
func collectClusterMetrics(
	ctx context.Context,
	bw *bundleWriter,
	cc *checks.CheckContext,
	fetcher metricsFetcher,
	opts MetricsOptions,
	progress func(format string, args ...any),
) []error {
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

	if opts.Samples < 1 {
		opts.Samples = 1
	}

	var errs []error
	for i := 0; i < opts.Samples; i++ {
		if i > 0 {
			// Wait between samples but respect cancellation so a
			// long-running bundle can be aborted cleanly.
			timer := time.NewTimer(opts.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return errs
			case <-timer.C:
			}
		}
		if progress != nil {
			progress("[%s] /metrics sample %d/%d", cc.Context, i+1, opts.Samples)
		}
		data, err := fetcher.Metrics(ctx, cc.Namespace, cc.Pod.Name, scheme, port)
		if err != nil {
			errs = append(errs, fmt.Errorf("scraping metrics from %s/%s sample %d/%d (%s://:%d): %w",
				cc.Pod.Namespace, cc.Pod.Name, i+1, opts.Samples, scheme, port, err))
			continue
		}
		entry := path.Join("clusters", cc.Context, "metrics", fmt.Sprintf("t%d_metrics.txt", i))
		if werr := bw.writeBytes(entry, data); werr != nil {
			errs = append(errs, werr)
		}
	}
	return errs
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
