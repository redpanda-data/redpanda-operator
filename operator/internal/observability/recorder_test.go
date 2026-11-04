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
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

var (
	v1Ref   = redpandav1alpha2.ClusterRef{Group: ptr.To("redpanda.vectorized.io"), Kind: ptr.To("Cluster"), Name: "rp"}
	v2Ref   = redpandav1alpha2.ClusterRef{Name: "rp"}
	poolRef = redpandav1alpha2.ClusterRef{Kind: ptr.To(redpandav1alpha2.NodePoolKind), Name: "pool"}
)

// A new BrokerPhase has to be added to brokerPhases, and to the gauge's docs
// and suggested alerts; until it is, brokers in it count as Unknown.
func TestBrokerPhasesMatchAPI(t *testing.T) {
	want := []string{BrokerPhaseUnknown}
	for _, p := range redpandav1alpha2.BrokerPhases() {
		want = append(want, string(p))
	}
	assert.ElementsMatch(t, want, brokerPhases)
}

func TestBrokerPhaseCounts(t *testing.T) {
	key := func(kind, name string) BrokerClusterKey {
		return BrokerClusterKey{Namespace: "ns", Kind: kind, Name: name}
	}
	for _, tc := range []struct {
		name    string
		kind    string
		brokers []redpandav1alpha2.Broker
		// Only non-zero phases; every key is also checked to carry all phases.
		want map[BrokerClusterKey]map[string]float64
	}{
		{
			name: "v1 brokers count for kind Cluster only",
			kind: BrokerKindCluster,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v1Ref, "rp", redpandav1alpha2.BrokerPhaseRunning),
				testBroker("ns", v1Ref, "rp", redpandav1alpha2.BrokerPhaseRunning),
				testBroker("ns", v2Ref, "rp", redpandav1alpha2.BrokerPhaseRunning),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindCluster, "rp"): {"Running": 2},
			},
		},
		{
			name: "v2 and nodepool brokers roll up to their Redpanda",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v2Ref, "rp", redpandav1alpha2.BrokerPhaseRunning),
				testBroker("ns", poolRef, "rp", redpandav1alpha2.BrokerPhaseStuck),
				testBroker("ns", v1Ref, "rp", redpandav1alpha2.BrokerPhaseRunning),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindRedpanda, "rp"): {"Running": 1, "Stuck": 1},
			},
		},
		{
			name: "cluster name falls back to the clusterRef",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v2Ref, "", redpandav1alpha2.BrokerPhaseProvisioning),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindRedpanda, "rp"): {"Provisioning": 1},
			},
		},
		{
			name: "nodepool broker without the cluster label is skipped",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", poolRef, "", redpandav1alpha2.BrokerPhaseRunning),
			},
			want: map[BrokerClusterKey]map[string]float64{},
		},
		{
			name: "empty phase counts as Unknown",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v2Ref, "rp", ""),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindRedpanda, "rp"): {BrokerPhaseUnknown: 1},
			},
		},
		{
			name: "a phase this operator does not know counts as Unknown",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v2Ref, "rp", "Rebalancing"),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindRedpanda, "rp"): {BrokerPhaseUnknown: 1},
			},
		},
		{
			name: "same name in two namespaces is two clusters",
			kind: BrokerKindRedpanda,
			brokers: []redpandav1alpha2.Broker{
				testBroker("ns", v2Ref, "rp", redpandav1alpha2.BrokerPhaseDiskLost),
				testBroker("other", v2Ref, "rp", redpandav1alpha2.BrokerPhaseDecommissioned),
			},
			want: map[BrokerClusterKey]map[string]float64{
				key(BrokerKindRedpanda, "rp"):                              {"DiskLost": 1},
				{Namespace: "other", Kind: BrokerKindRedpanda, Name: "rp"}: {"Decommissioned": 1},
			},
		},
		{
			name: "no brokers",
			kind: BrokerKindRedpanda,
			want: map[BrokerClusterKey]map[string]float64{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := brokerPhaseCounts(tc.brokers, tc.kind)
			require.Len(t, got, len(tc.want))
			for k, phases := range got {
				assert.Len(t, phases, len(brokerPhases), "%v is not zero-filled", k)
				nonZero := map[string]float64{}
				for phase, n := range phases {
					if n != 0 {
						nonZero[phase] = n
					}
				}
				assert.Equal(t, tc.want[k], nonZero, "%v", k)
			}
		})
	}
}

func TestBrokerPhaseRecorder(t *testing.T) {
	t.Cleanup(BrokerPhase.Reset)
	const metric = "operator_broker_phase"
	series := func(name, phase string) map[string]string {
		return map[string]string{"namespace": "rec", "kind": BrokerKindRedpanda, "name": name, "phase": phase}
	}
	a := testBroker("rec", v2Ref, "a", redpandav1alpha2.BrokerPhaseRunning)
	b := testBroker("rec", v2Ref, "b", redpandav1alpha2.BrokerPhaseStuck)

	r := NewBrokerPhaseRecorder(BrokerKindRedpanda)
	r.Record([]redpandav1alpha2.Broker{a, b})
	assert.Equal(t, 1.0, readGauge(t, metric, series("a", "Running")))
	assert.Equal(t, 1.0, readGauge(t, metric, series("b", "Stuck")))
	assert.True(t, hasGauge(t, metric, series("a", "Stuck")), "zero-filled phase must be exported")

	// A cluster whose brokers are gone loses every series, not just the
	// non-zero ones.
	r.Record([]redpandav1alpha2.Broker{a})
	assert.True(t, hasGauge(t, metric, series("a", "Running")))
	for _, phase := range brokerPhases {
		assert.False(t, hasGauge(t, metric, series("b", phase)), "b/%s still exported", phase)
	}

	// Re-recording the same input is a no-op.
	r.Record([]redpandav1alpha2.Broker{a})
	assert.Equal(t, 1.0, readGauge(t, metric, series("a", "Running")))

	// A recorder of the other kind owns disjoint series and must not touch
	// these.
	NewBrokerPhaseRecorder(BrokerKindCluster).Record(nil)
	assert.True(t, hasGauge(t, metric, series("a", "Running")))

	r.Record(nil)
	assert.False(t, hasGauge(t, metric, series("a", "Running")))
}

// A Broker List that never returns must not wedge the controller: Observe
// gives up after observeTimeout and the series keep their last values.
func TestBrokerPhaseRecorderObserveTimeout(t *testing.T) {
	t.Cleanup(BrokerPhase.Reset)
	restore := observeTimeout
	observeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { observeTimeout = restore })

	scheme := runtime.NewScheme()
	require.NoError(t, redpandav1alpha2.AddToScheme(scheme))
	// Nothing reaches the host: typed reads go to the cache reader, which is
	// what blocks in production while an informer waits to sync.
	ctl, err := kube.FromRESTConfig(&kube.RESTConfig{Host: "http://127.0.0.1:1"}, kube.Options{Options: client.Options{
		Scheme: scheme,
		Cache:  &client.CacheOptions{Reader: neverSyncingReader{}},
	}})
	require.NoError(t, err)

	r := NewBrokerPhaseRecorder(BrokerKindRedpanda)
	r.Record([]redpandav1alpha2.Broker{testBroker("rec", v2Ref, "a", redpandav1alpha2.BrokerPhaseRunning)})

	start := time.Now()
	require.Error(t, r.Observe(t.Context(), ctl))
	require.Less(t, time.Since(start), 5*time.Second)
	assert.Equal(t, 1.0, readGauge(t, "operator_broker_phase", map[string]string{"namespace": "rec", "kind": BrokerKindRedpanda, "name": "a", "phase": "Running"}))
}

// neverSyncingReader is a cache whose List blocks until the context ends.
type neverSyncingReader struct{ client.Reader }

func (neverSyncingReader) List(ctx context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	<-ctx.Done()
	return ctx.Err()
}

func testBroker(namespace string, ref redpandav1alpha2.ClusterRef, clusterLabel string, phase redpandav1alpha2.BrokerPhase) redpandav1alpha2.Broker {
	b := redpandav1alpha2.Broker{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace},
		Spec:       redpandav1alpha2.BrokerSpec{ClusterRef: ref},
		Status:     redpandav1alpha2.BrokerStatus{Phase: phase},
	}
	if clusterLabel != "" {
		b.Labels = map[string]string{redpandav1alpha2.ClusterNameLabel: clusterLabel}
	}
	return b
}

func hasGauge(t *testing.T, name string, labels map[string]string) bool {
	t.Helper()
	for _, fam := range gather(t) {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			if labelsContain(m.GetLabel(), labels) {
				return true
			}
		}
	}
	return false
}
