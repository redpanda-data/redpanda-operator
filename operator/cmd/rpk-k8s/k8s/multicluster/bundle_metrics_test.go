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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

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
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher)
	require.NoError(t, bw.Close())
	require.Empty(t, errs)

	// One scrape, against the right pod and port.
	require.Len(t, fetcher.calls, 1)
	assert.Equal(t, metricsCall{Namespace: "redpanda", PodName: "operator-0", Scheme: "http", Port: 8443}, fetcher.calls[0])

	// metrics.txt must contain the canned body verbatim.
	files := readZipFilesInternal(t, buf.Bytes())
	require.Contains(t, files, "clusters/self/metrics/metrics.txt")
	assert.Equal(t, string(body), string(files["clusters/self/metrics/metrics.txt"]))
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
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher)
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
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher)
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
	errs := collectClusterMetrics(context.Background(), bw, cc, fetcher)
	require.NoError(t, bw.Close())
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "simulated 403 forbidden")
	assert.Contains(t, errs[0].Error(), "operator-0")

	// metrics.txt must NOT be present in the zip when the scrape failed.
	files := readZipFilesInternal(t, buf.Bytes())
	for fname := range files {
		assert.NotContains(t, fname, "/metrics/metrics.txt")
	}
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
	assert.Equal(t, "http", metricsScheme([]string{"--metrics-bind-address=:8443"}))
	assert.Equal(t, "https", metricsScheme([]string{
		"--metrics-bind-address=:8443",
		"--metrics-cert-path=/tls/tls.crt",
	}))
}
