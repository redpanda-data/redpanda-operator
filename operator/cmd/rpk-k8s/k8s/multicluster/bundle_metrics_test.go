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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// singleSampleOpts is the simplest opts that satisfies collectClusterMetrics
// in the tests that only care about a single scrape. Two samples is the
// command-line default; tests that exercise multi-sample behaviour use a
// short interval to keep wall-clock time low.
var singleSampleOpts = MetricsOptions{Samples: 1, Interval: time.Millisecond}

// fakeMetricsFetcher records its scrape arguments and returns a canned
// response. Tests assert on the recorded calls and on the bytes that ended
// up in the bundle zip.
type fakeMetricsFetcher struct {
	body  []byte
	err   error
	calls []metricsCall
}

type metricsCall struct {
	Namespace string
	PodName   string
	Scheme    string
	Port      int
}

func (f *fakeMetricsFetcher) Metrics(_ context.Context, namespace, podName, scheme string, port int) ([]byte, error) {
	f.calls = append(f.calls, metricsCall{Namespace: namespace, PodName: podName, Scheme: scheme, Port: port})
	if f.err != nil {
		return nil, f.err
	}
	return f.body, nil
}

// fetcherWithCounter returns a different body (or error) on each call —
// used by the multi-sample tests to verify per-sample distinct content
// lands in distinct files.
type fetcherWithCounter struct {
	cb func() ([]byte, error)
}

func (f *fetcherWithCounter) Metrics(_ context.Context, _, _, _ string, _ int) ([]byte, error) {
	return f.cb()
}

func TestCollectClusterMetrics_HappyPath(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"},
	}
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        pod,
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}

	body := []byte("# HELP example_total help text\nexample_total 1\n")
	fetcher := &fakeMetricsFetcher{body: body}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, singleSampleOpts, nil)
	require.NoError(t, bw.Close())
	require.Empty(t, errs)

	// One scrape, against the right pod and port.
	require.Len(t, fetcher.calls, 1)
	assert.Equal(t, metricsCall{Namespace: "redpanda", PodName: "operator-0", Scheme: "https", Port: 8443}, fetcher.calls[0])

	// t0_metrics.txt must contain the canned body verbatim.
	files := readZipFilesInternal(t, buf.Bytes())
	require.Contains(t, files, "clusters/self/metrics/t0_metrics.txt")
	assert.Equal(t, string(body), string(files["clusters/self/metrics/t0_metrics.txt"]))
}

func TestCollectClusterMetrics_MultipleSamples(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"},
	}
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        pod,
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}

	// Each call returns a different body so we can verify per-sample
	// content lands in its own file.
	var calls int
	fetcher := &fetcherWithCounter{cb: func() ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf("sample %d\n", calls)), nil
	}}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	opts := MetricsOptions{Samples: 3, Interval: time.Millisecond}
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, opts, nil)
	require.NoError(t, bw.Close())
	require.Empty(t, errs)
	assert.Equal(t, 3, calls, "fetcher must be called once per sample")

	files := readZipFilesInternal(t, buf.Bytes())
	require.Contains(t, files, "clusters/self/metrics/t0_metrics.txt")
	require.Contains(t, files, "clusters/self/metrics/t1_metrics.txt")
	require.Contains(t, files, "clusters/self/metrics/t2_metrics.txt")
	assert.Equal(t, "sample 1\n", string(files["clusters/self/metrics/t0_metrics.txt"]))
	assert.Equal(t, "sample 2\n", string(files["clusters/self/metrics/t1_metrics.txt"]))
	assert.Equal(t, "sample 3\n", string(files["clusters/self/metrics/t2_metrics.txt"]))
}

func TestCollectClusterMetrics_PartialFailureContinues(t *testing.T) {
	// Sample 2 fails but sample 1 and 3 succeed. The error must be
	// recorded and the surviving samples must be in the bundle.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"},
	}
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        pod,
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}

	var calls int
	fetcher := &fetcherWithCounter{cb: func() ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("transient: connection reset")
		}
		return []byte(fmt.Sprintf("sample %d\n", calls)), nil
	}}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	opts := MetricsOptions{Samples: 3, Interval: time.Millisecond}
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, opts, nil)
	require.NoError(t, bw.Close())
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "sample 2/3")
	assert.Contains(t, errs[0].Error(), "transient: connection reset")

	files := readZipFilesInternal(t, buf.Bytes())
	require.Contains(t, files, "clusters/self/metrics/t0_metrics.txt")
	assert.NotContains(t, files, "clusters/self/metrics/t1_metrics.txt", "failed sample must not produce a file")
	require.Contains(t, files, "clusters/self/metrics/t2_metrics.txt")
}

func TestCollectClusterMetrics_HTTPSWhenCertPathSet(t *testing.T) {
	cc := &checks.CheckContext{
		Context:   "self",
		Namespace: "redpanda",
		Pod:       &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"}},
		DeployArgs: []string{
			"--metrics-bind-address=:8443",
			"--metrics-cert-path=/tls/tls.crt",
			"--metrics-key-path=/tls/tls.key",
		},
	}
	fetcher := &fakeMetricsFetcher{body: []byte("ok")}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, singleSampleOpts, nil)
	require.NoError(t, bw.Close())
	require.Empty(t, errs)
	require.Len(t, fetcher.calls, 1)
	assert.Equal(t, "https", fetcher.calls[0].Scheme,
		"--metrics-cert-path should select the https proxy scheme")
}

func TestCollectClusterMetrics_MissingFlagSkipsCleanly(t *testing.T) {
	// Operator deployed without --metrics-bind-address — metrics server
	// disabled; collectClusterMetrics must be a no-op (no error, no
	// fetch, no bundle entry).
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"}},
		DeployArgs: []string{"--leader-elect"},
	}
	fetcher := &fakeMetricsFetcher{body: []byte("should-not-be-fetched")}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, singleSampleOpts, nil)
	require.NoError(t, bw.Close())
	assert.Empty(t, errs)
	assert.Empty(t, fetcher.calls, "fetcher must not be called when --metrics-bind-address is unset")
}

func TestCollectClusterMetrics_ErrorIsRecorded(t *testing.T) {
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"}},
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}
	fetcher := &fakeMetricsFetcher{err: fmt.Errorf("simulated 403 forbidden")}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, singleSampleOpts, nil)
	require.NoError(t, bw.Close())
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "simulated 403 forbidden")
	assert.Contains(t, errs[0].Error(), "operator-0")

	// No t<i>_metrics.txt entry should exist when the only sample failed.
	files := readZipFilesInternal(t, buf.Bytes())
	for fname := range files {
		assert.NotContains(t, fname, "/metrics/t")
	}
}

// schemeAwareFetcher answers only over `serves` and records every attempt,
// standing in for an operator whose actual TLS mode disagrees with what its
// deployment args imply.
type schemeAwareFetcher struct {
	serves  string
	schemes []string
}

func (f *schemeAwareFetcher) Metrics(_ context.Context, _, _, scheme string, _ int) ([]byte, error) {
	f.schemes = append(f.schemes, scheme)
	if scheme != f.serves {
		return nil, fmt.Errorf("tls: first record does not look like a TLS handshake")
	}
	return []byte("ok"), nil
}

// TestCollectClusterMetrics_FallsBackToOtherScheme covers scraping an operator
// old enough to serve plain HTTP for args that now imply HTTPS. The args say
// https, the server speaks http, and the bundle must still get its samples —
// with the working scheme reused rather than re-probed for every sample.
func TestCollectClusterMetrics_FallsBackToOtherScheme(t *testing.T) {
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"}},
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}
	fetcher := &schemeAwareFetcher{serves: "http"}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, MetricsOptions{Samples: 2, Interval: time.Millisecond}, nil)
	require.NoError(t, bw.Close())
	require.Empty(t, errs)

	assert.Equal(t, []string{"https", "http", "http"}, fetcher.schemes)

	files := readZipFilesInternal(t, buf.Bytes())
	assert.Contains(t, files, "clusters/self/metrics/t0_metrics.txt")
	assert.Contains(t, files, "clusters/self/metrics/t1_metrics.txt")
}

// TestCollectClusterMetrics_FallbackDoesNotMaskRealErrors pins that the
// fallback is a one-shot scheme probe, not a blanket retry: an authorization
// failure still surfaces, and once a scheme is known to work a later failure
// isn't re-probed.
func TestCollectClusterMetrics_FallbackDoesNotMaskRealErrors(t *testing.T) {
	cc := &checks.CheckContext{
		Context:    "self",
		Namespace:  "redpanda",
		Pod:        &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"}},
		DeployArgs: []string{"--metrics-bind-address=:8443"},
	}
	fetcher := &schemeAwareFetcher{serves: "neither"}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher, singleSampleOpts, nil)
	require.NoError(t, bw.Close())

	require.Len(t, errs, 1, "a scheme that never answers must still be reported once")
	assert.Contains(t, errs[0].Error(), "TLS handshake")
	assert.Equal(t, []string{"https", "http"}, fetcher.schemes, "exactly one fallback attempt")
}

func TestParseMetricsPort(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantPort int
		wantOK   bool
	}{
		{name: "default :8443", args: []string{"--metrics-bind-address=:8443"}, wantPort: 8443, wantOK: true},
		{name: "host:port", args: []string{"--metrics-bind-address=0.0.0.0:9090"}, wantPort: 9090, wantOK: true},
		{name: "missing flag", args: []string{"--leader-elect"}, wantPort: 0, wantOK: false},
		{name: "empty value disables", args: []string{"--metrics-bind-address="}, wantPort: 0, wantOK: false},
		{name: "garbage value", args: []string{"--metrics-bind-address=:abc"}, wantPort: 0, wantOK: false},
		{name: "out of range", args: []string{"--metrics-bind-address=:99999"}, wantPort: 0, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port, ok := parseMetricsPort(tc.args)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantPort, port)
		})
	}
}

func TestMetricsScheme(t *testing.T) {
	// --metrics-secure defaults to true in both `run` and `multicluster`, so
	// bare args mean TLS with a self-signed certificate.
	assert.Equal(t, "https", metricsScheme([]string{"--metrics-bind-address=:8443"}))
	assert.Equal(t, "https", metricsScheme([]string{
		"--metrics-bind-address=:8443",
		"--metrics-cert-path=/tls/tls.crt",
	}))
	assert.Equal(t, "http", metricsScheme([]string{
		"--metrics-bind-address=:8443",
		"--metrics-secure=false",
	}))
	// An explicit certificate wins over --metrics-secure=false, matching the
	// server: a cert path implies secure serving.
	assert.Equal(t, "https", metricsScheme([]string{
		"--metrics-bind-address=:8443",
		"--metrics-secure=false",
		"--metrics-cert-path=/tls/tls.crt",
	}))
}
