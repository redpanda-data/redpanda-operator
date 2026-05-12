// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// stubReconciler returns a canned (Result, error) for every call. Used to
// drive the wrapper's record() through each metric-relevant code path
// without needing a real controller.
type stubReconciler struct {
	result reconcile.Result
	err    error
	calls  int
}

func (s *stubReconciler) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	s.calls++
	return s.result, s.err
}

func TestWrap_SteadyState_CountsZeroResultNoError(t *testing.T) {
	stub := &stubReconciler{result: reconcile.Result{}, err: nil}
	w := Wrap[reconcile.Request](stub, "TestController_Steady")

	before := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_Steady"})
	_, err := w.Reconcile(context.Background(), reconcile.Request{NamespacedName: ctrl.ObjectKey{Name: "x"}})
	require.NoError(t, err)
	after := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_Steady"})

	assert.Equal(t, 1, stub.calls, "inner reconciler must be invoked exactly once")
	assert.InDelta(t, before+1, after, 0.0001, "steady_state counter must increment on a (Result{}, nil) return")
}

func TestWrap_SteadyState_DoesNotCountErrorReturn(t *testing.T) {
	stub := &stubReconciler{result: reconcile.Result{}, err: errors.New("boom")}
	w := Wrap[reconcile.Request](stub, "TestController_Error")

	before := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_Error"})
	_, err := w.Reconcile(context.Background(), reconcile.Request{})
	require.Error(t, err)
	after := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_Error"})

	assert.InDelta(t, before, after, 0.0001, "error returns must not be counted as steady-state")
}

func TestWrap_SteadyState_DoesNotCountRequeueReturn(t *testing.T) {
	// A reconcile that asks to be re-queued (either with Requeue:true or
	// a RequeueAfter > 0) is *not* steady state. The controller is
	// explicitly saying "come back."
	stub := &stubReconciler{result: reconcile.Result{RequeueAfter: 5 * time.Second}, err: nil}
	w := Wrap[reconcile.Request](stub, "TestController_RequeueAfter")

	before := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_RequeueAfter"})
	_, err := w.Reconcile(context.Background(), reconcile.Request{})
	require.NoError(t, err)
	after := readCounter(t, "operator_controller_reconcile_steady_state_total", map[string]string{"controller": "TestController_RequeueAfter"})

	assert.InDelta(t, before, after, 0.0001, "RequeueAfter-returning reconciles must not be counted as steady-state")
}

func TestWrap_RequeueAfter_ObservesHistogram(t *testing.T) {
	stub := &stubReconciler{result: reconcile.Result{RequeueAfter: 30 * time.Second}, err: nil}
	w := Wrap[reconcile.Request](stub, "TestController_Histogram")

	before := readHistogramCount(t, "operator_controller_reconcile_requeue_after_seconds", map[string]string{"controller": "TestController_Histogram"})
	_, _ = w.Reconcile(context.Background(), reconcile.Request{})
	_, _ = w.Reconcile(context.Background(), reconcile.Request{})
	_, _ = w.Reconcile(context.Background(), reconcile.Request{})
	after := readHistogramCount(t, "operator_controller_reconcile_requeue_after_seconds", map[string]string{"controller": "TestController_Histogram"})

	assert.Equal(t, before+3, after, "histogram must record one observation per RequeueAfter-returning reconcile")
}

func TestWrap_RequeueAfter_SkipsZeroDuration(t *testing.T) {
	// Result{Requeue: true, RequeueAfter: 0} means "re-queue immediately."
	// We don't observe the histogram for it — the histogram is keyed on
	// the *delay*, and zero isn't a meaningful delay.
	stub := &stubReconciler{result: reconcile.Result{Requeue: true}, err: nil}
	w := Wrap[reconcile.Request](stub, "TestController_ImmediateRequeue")

	before := readHistogramCount(t, "operator_controller_reconcile_requeue_after_seconds", map[string]string{"controller": "TestController_ImmediateRequeue"})
	_, _ = w.Reconcile(context.Background(), reconcile.Request{})
	after := readHistogramCount(t, "operator_controller_reconcile_requeue_after_seconds", map[string]string{"controller": "TestController_ImmediateRequeue"})

	assert.Equal(t, before, after, "immediate-requeue must not produce a histogram observation")
}

func TestWrap_PreservesInnerResultAndError(t *testing.T) {
	wantResult := reconcile.Result{RequeueAfter: 7 * time.Second}
	wantErr := errors.New("inner error")
	stub := &stubReconciler{result: wantResult, err: wantErr}
	w := Wrap[reconcile.Request](stub, "TestController_Passthrough")

	gotResult, gotErr := w.Reconcile(context.Background(), reconcile.Request{})
	assert.Equal(t, wantResult, gotResult, "wrapper must return the inner reconciler's Result verbatim")
	assert.Equal(t, wantErr, gotErr, "wrapper must return the inner reconciler's error verbatim")
}

func TestRecordObservedGeneration_PositiveDrift(t *testing.T) {
	// Object spec has advanced (generation=5) but status hasn't caught up
	// (observedGeneration=3) — drift is 2.
	RecordObservedGeneration("TestController_Drift", "MyKind", 5, 3)
	got := readGauge(t, "operator_controller_reconcile_observed_generation_drift",
		map[string]string{"controller": "TestController_Drift", "kind": "MyKind"})
	assert.InDelta(t, 2, got, 0.0001)
}

func TestRecordObservedGeneration_ClampsNegativeToZero(t *testing.T) {
	// observedGeneration > generation can happen briefly during a
	// generation bump race. Clamp to 0 so the gauge stays semantically
	// "how far behind."
	RecordObservedGeneration("TestController_ClampNegative", "MyKind", 3, 5)
	got := readGauge(t, "operator_controller_reconcile_observed_generation_drift",
		map[string]string{"controller": "TestController_ClampNegative", "kind": "MyKind"})
	assert.InDelta(t, 0, got, 0.0001)
}

func TestRecordSpecHashChangedWithoutGeneration_Increments(t *testing.T) {
	before := readCounter(t, "operator_controller_reconcile_spec_hash_changed_without_generation_total",
		map[string]string{"controller": "TestController_NonDet", "kind": "MyKind"})
	RecordSpecHashChangedWithoutGeneration("TestController_NonDet", "MyKind")
	RecordSpecHashChangedWithoutGeneration("TestController_NonDet", "MyKind")
	after := readCounter(t, "operator_controller_reconcile_spec_hash_changed_without_generation_total",
		map[string]string{"controller": "TestController_NonDet", "kind": "MyKind"})
	assert.InDelta(t, before+2, after, 0.0001)
}

// readCounter scrapes controller-runtime's metrics registry and returns the
// current value of the named counter for the given label set. Returns 0
// when the family or labelled series is absent (i.e. before the first
// increment).
func readCounter(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	families := gather(t)
	for _, fam := range families {
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
	families := gather(t)
	for _, fam := range families {
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
	families := gather(t)
	for _, fam := range families {
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
