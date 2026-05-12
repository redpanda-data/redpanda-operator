// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package observability_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/redpanda-data/redpanda-operator/operator/internal/observability"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// pathology is what the test reconciler does on its next call.
type pathology int32

const (
	pathSteady   pathology = iota // return (Result{}, nil) — healthy
	pathSpinning                  // return (Result{RequeueAfter: 100ms}, nil) — infinite-reconcile
)

// testReconciler reconciles ConfigMaps with configurable behaviour. The
// behaviour is held in an atomic so a test can switch from spinning to
// steady-state mid-flight without restarting the manager.
type testReconciler struct {
	client     ctrlclient.Client
	mode       *atomic.Int32
	callCount  *atomic.Int64
	controller string
}

func (r *testReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	r.callCount.Add(1)
	switch pathology(r.mode.Load()) {
	case pathSpinning:
		return reconcile.Result{RequeueAfter: 100 * time.Millisecond}, nil
	default:
		return reconcile.Result{}, nil
	}
}

// TestIntegrationObservabilityInfiniteReconcile validates the operator
// observability wrapper end-to-end:
//
//   - Spinning phase: a reconciler that always returns RequeueAfter <1s
//     must produce a requeue_after_seconds histogram populated in the
//     low buckets, must NOT advance steady_state_total, and must NOT
//     advance last_success_timestamp_seconds.
//   - Recovery phase: switching the reconciler to (Result{}, nil) must
//     start accruing steady_state_total and must advance
//     last_success_timestamp_seconds to a current timestamp.
//
// The test exercises the wrapper inside a real controller-runtime
// Manager driven by a real envtest apiserver — this catches breakage
// that unit tests with a stub reconciler would miss (e.g., the wrapper
// not being invoked from the controller-runtime reconcile loop).
func TestIntegrationObservabilityInfiniteReconcile(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	env := &envtest.Environment{}
	cfg, err := env.Start()
	require.NoError(t, err, "envtest start")
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(cfg, manager.Options{
		Scheme: scheme,
		// Disable the manager's own metrics HTTP server — we gather
		// directly from the controller-runtime registry below. This
		// keeps the test from racing for a port on a CI runner.
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	require.NoError(t, err, "manager init")

	controllerName := "ObservabilityIntegration"
	mode := &atomic.Int32{}
	calls := &atomic.Int64{}
	mode.Store(int32(pathSpinning))

	reconciler := &testReconciler{
		client:     mgr.GetClient(),
		mode:       mode,
		callCount:  calls,
		controller: controllerName,
	}
	wrapped := observability.Wrap[reconcile.Request](reconciler, controllerName, 0)

	err = ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&corev1.ConfigMap{}).
		Complete(wrapped)
	require.NoError(t, err, "controller setup")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgrErr := make(chan error, 1)
	go func() { mgrErr <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-mgrErr:
		case <-time.After(5 * time.Second):
		}
	})

	// Apply a ConfigMap to wake the controller.
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "obs-trigger", Namespace: "default"},
		Data:       map[string]string{"k": "v"},
	}
	require.NoError(t, mgr.GetClient().Create(ctx, cm))

	// Wait for the reconciler to spin a few times. In spinning mode it
	// re-enqueues itself every 100ms, so 2s is plenty.
	require.Eventually(t, func() bool { return calls.Load() >= 3 }, 5*time.Second, 50*time.Millisecond,
		"reconciler must be invoked at least 3 times in spinning phase")

	// -- Spinning-phase assertions --
	steady := readCounter(t, "operator_controller_reconcile_steady_state_total",
		map[string]string{"controller": controllerName})
	assert.InDelta(t, 0, steady, 0.0001, "steady_state_total must stay at 0 while reconciler is spinning")

	requeueCount := readHistogramCount(t, "operator_controller_reconcile_requeue_after_seconds",
		map[string]string{"controller": controllerName})
	assert.GreaterOrEqual(t, requeueCount, uint64(3),
		"requeue_after_seconds histogram must record one observation per spinning reconcile")

	lastSuccess := readGauge(t, "operator_controller_reconcile_last_success_timestamp_seconds",
		map[string]string{"controller": controllerName})
	assert.InDelta(t, 0, lastSuccess, 0.0001,
		"last_success_timestamp_seconds must stay at 0 — no steady-state reconcile has happened yet")

	// -- Recovery phase --
	preSwitchCalls := calls.Load()
	mode.Store(int32(pathSteady))

	// Mutate the ConfigMap to trigger at least one fresh reconcile in
	// the new mode. The RequeueAfter from the last spinning call also
	// fires within 100ms — either path is fine.
	require.NoError(t, mgr.GetClient().Get(ctx, ctrlclient.ObjectKeyFromObject(cm), cm))
	cm.Data["k"] = "v2"
	require.NoError(t, mgr.GetClient().Update(ctx, cm))

	beforeRecovery := time.Now().Unix()
	require.Eventually(t, func() bool {
		return readCounter(t, "operator_controller_reconcile_steady_state_total",
			map[string]string{"controller": controllerName}) >= 1
	}, 5*time.Second, 50*time.Millisecond,
		"steady_state_total must accrue once the reconciler returns (Result{}, nil)")

	lastSuccess = readGauge(t, "operator_controller_reconcile_last_success_timestamp_seconds",
		map[string]string{"controller": controllerName})
	assert.GreaterOrEqual(t, int64(lastSuccess), beforeRecovery-1,
		"last_success_timestamp_seconds must hold a recent timestamp after recovery")

	assert.Greater(t, calls.Load(), preSwitchCalls, "reconciler must be invoked again after the recovery write")
}

// readCounter / readGauge / readHistogramCount / labelsContain are
// duplicated here from wrapper_test.go because that file is in the
// internal `observability` package and this test runs in the external
// `observability_test` package to exercise the public Wrap API.

func readCounter(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	for _, fam := range gather(t) {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if labelsContain(m.GetLabel(), labels) {
				if c := m.GetCounter(); c != nil {
					return c.GetValue()
				}
			}
		}
	}
	return 0
}

func readGauge(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	for _, fam := range gather(t) {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if labelsContain(m.GetLabel(), labels) {
				if g := m.GetGauge(); g != nil {
					return g.GetValue()
				}
			}
		}
	}
	return 0
}

func readHistogramCount(t *testing.T, name string, labels map[string]string) uint64 {
	t.Helper()
	for _, fam := range gather(t) {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if labelsContain(m.GetLabel(), labels) {
				if h := m.GetHistogram(); h != nil {
					return h.GetSampleCount()
				}
			}
		}
	}
	return 0
}

func gather(t *testing.T) []*dto.MetricFamily {
	t.Helper()
	gatherer, ok := ctrlmetrics.Registry.(prometheus.Gatherer)
	require.True(t, ok, "controller-runtime metrics.Registry must implement prometheus.Gatherer")
	families, err := gatherer.Gather()
	require.NoError(t, err)
	return families
}

func labelsContain(labels []*dto.LabelPair, want map[string]string) bool {
	for k, v := range want {
		found := false
		for _, lp := range labels {
			if lp.GetName() == k && lp.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
