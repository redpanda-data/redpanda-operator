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
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// fakeLogFetcher is a deterministic logFetcher used by Phase 2 unit tests.
// It records the exact (podName, container, opts) tuples it was asked for
// and serves canned responses keyed by container name. Every call appends a
// trailer that distinguishes current from previous logs so tests can assert
// the right `Previous` flag was passed.
type fakeLogFetcher struct {
	// responses maps container name -> static body returned for the
	// current-log fetch (Previous=false). The previous-log body is the
	// same body with a "[previous]" suffix.
	responses map[string]string
	// fail maps "container#previous=<bool>" -> error to return on that
	// fetch. Empty means succeed.
	fail map[string]error
	// calls records every Logs invocation. Tests assert on this.
	calls []logCall
}

type logCall struct {
	Namespace string
	PodName   string
	Container string
	Previous  bool
	Limit     int64 // 0 if unset
	Tail      int64 // 0 if unset
}

func (f *fakeLogFetcher) Logs(_ context.Context, namespace, podName, container string, opts *corev1.PodLogOptions) ([]byte, error) {
	c := logCall{
		Namespace: namespace,
		PodName:   podName,
		Container: container,
		Previous:  opts.Previous,
	}
	if opts.LimitBytes != nil {
		c.Limit = *opts.LimitBytes
	}
	if opts.TailLines != nil {
		c.Tail = *opts.TailLines
	}
	f.calls = append(f.calls, c)

	key := fmt.Sprintf("%s#previous=%v", container, opts.Previous)
	if err, ok := f.fail[key]; ok {
		return nil, err
	}

	body, ok := f.responses[container]
	if !ok {
		body = ""
	}
	if opts.Previous {
		body += "[previous]"
	}
	return []byte(body), nil
}

func TestCollectClusterLogs_HappyPath(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "init"}},
			Containers:     []corev1.Container{{Name: "manager"}, {Name: "sidecar"}},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{Name: "init", RestartCount: 0},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				// manager has restarted once → "previous" log should be fetched.
				{Name: "manager", RestartCount: 2},
				{Name: "sidecar", RestartCount: 0},
			},
		},
	}
	cc := &checks.CheckContext{
		Context:   "self",
		Namespace: "redpanda",
		Pod:       pod,
	}
	fetcher := &fakeLogFetcher{
		responses: map[string]string{
			"init":    "init log body",
			"manager": "manager log body",
			"sidecar": "sidecar log body",
		},
	}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterLogs(context.Background(), bw, cc, fetcher,
		LogsOptions{LimitBytes: 1024, TailLines: 100})
	require.Empty(t, errs, "happy path should produce no errors")
	require.NoError(t, bw.Close())

	// Expected file set: every container's current log, plus manager.previous
	// because that's the only container with RestartCount > 0.
	files := readZipFilesInternal(t, buf.Bytes())
	got := sortedKeys(files)
	want := []string{
		"clusters/self/logs/init.log",
		"clusters/self/logs/manager.log",
		"clusters/self/logs/manager.previous.log",
		"clusters/self/logs/sidecar.log",
	}
	assert.Equal(t, want, got)

	assert.Equal(t, "manager log body", string(files["clusters/self/logs/manager.log"]))
	assert.Equal(t, "manager log body[previous]", string(files["clusters/self/logs/manager.previous.log"]))

	// Verify the LogsOptions caps were forwarded to the fetcher.
	for _, c := range fetcher.calls {
		assert.Equal(t, int64(1024), c.Limit, "LimitBytes must propagate")
		assert.Equal(t, int64(100), c.Tail, "TailLines must propagate")
		assert.Equal(t, "redpanda", c.Namespace)
		assert.Equal(t, "operator-0", c.PodName)
	}
	// 4 calls expected: init, manager, manager(prev), sidecar.
	require.Len(t, fetcher.calls, 4)
}

func TestCollectClusterLogs_PerContainerErrorIsRecorded(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-0", Namespace: "redpanda"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "manager"}, {Name: "sidecar"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "manager", RestartCount: 0},
				{Name: "sidecar", RestartCount: 0},
			},
		},
	}
	cc := &checks.CheckContext{Context: "self", Namespace: "redpanda", Pod: pod}
	fetcher := &fakeLogFetcher{
		responses: map[string]string{
			"manager": "manager log body",
			// sidecar deliberately omitted — fail path below.
		},
		fail: map[string]error{
			"sidecar#previous=false": fmt.Errorf("simulated apiserver error"),
		},
	}

	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterLogs(context.Background(), bw, cc, fetcher, LogsOptions{})
	require.NoError(t, bw.Close())

	require.Len(t, errs, 1, "exactly one container should fail")
	assert.Contains(t, errs[0].Error(), "sidecar")
	assert.Contains(t, errs[0].Error(), "simulated apiserver error")

	// The successful container's log should still be in the zip — a single
	// failing container can't lose the whole bundle.
	files := readZipFilesInternal(t, buf.Bytes())
	assert.Contains(t, files, "clusters/self/logs/manager.log")
	assert.NotContains(t, files, "clusters/self/logs/sidecar.log")
}

func TestCollectClusterLogs_NoPodNoOp(t *testing.T) {
	// PodCheck didn't find a pod (cc.Pod nil) — collectClusterLogs should
	// be a no-op rather than panic. The bundle still completes; users see
	// the upstream PodCheck failure in clusters/<ctx>/checks.json.
	cc := &checks.CheckContext{Context: "self", Namespace: "redpanda"}
	var buf bytes.Buffer
	bw := newBundleWriter(&buf)
	errs := collectClusterLogs(context.Background(), bw, cc, &fakeLogFetcher{}, LogsOptions{})
	require.NoError(t, bw.Close())
	assert.Empty(t, errs)
}

func TestResolveLogsOptions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         BundleConfig
		wantBytes   int64
		wantTail    int64
		wantErrSubs string // non-empty asserts an error containing this substring
	}{
		{
			name:      "empty inputs use defaults",
			cfg:       BundleConfig{},
			wantBytes: defaultLogsOptions().LimitBytes,
			wantTail:  defaultLogsOptions().TailLines,
		},
		{
			name:      "explicit human size and tail",
			cfg:       BundleConfig{LogsSizeLimit: "10MB", LogsTailLines: 200},
			wantBytes: 10_000_000,
			wantTail:  200,
		},
		{
			name:      "binary suffix accepted",
			cfg:       BundleConfig{LogsSizeLimit: "10MiB", LogsTailLines: 200},
			wantBytes: 10 * 1024 * 1024,
			wantTail:  200,
		},
		{
			name:      "0 disables size cap",
			cfg:       BundleConfig{LogsSizeLimit: "0", LogsTailLines: 0},
			wantBytes: 0,
			wantTail:  defaultLogsOptions().TailLines,
		},
		{
			name:      "negative tail clamped to 0",
			cfg:       BundleConfig{LogsTailLines: -1},
			wantBytes: defaultLogsOptions().LimitBytes,
			wantTail:  0,
		},
		{
			name:        "unparseable size is an error",
			cfg:         BundleConfig{LogsSizeLimit: "definitely not a number"},
			wantErrSubs: "logs-size-limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.cfg.resolveLogsOptions()
			if tc.wantErrSubs != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrSubs)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantBytes, out.LimitBytes)
			assert.Equal(t, tc.wantTail, out.TailLines)
		})
	}
}

// readZipFilesInternal mirrors readZipFiles in bundle_test.go but lives in
// the internal package so internal-package tests don't depend on the
// external test file. Test helper.
func readZipFilesInternal(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		require.NoError(t, err)
		out[f.Name] = data
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
