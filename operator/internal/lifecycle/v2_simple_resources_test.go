// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/endpointsteering"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
)

// TestV2GetAdminAPIEndpoints pins the per-pod admin endpoint contract the
// remediation reconcilers (stale-disk wipe, pod-identity ghost clears) rely
// on: one endpoint per desired pod, whose FIRST DNS LABEL IS THE POD NAME and
// whose host matches the internal RPC address the broker itself advertises
// (same headless-service domain the chart renders into the broker config),
// with the admin listener port. GetAdminAPIEndpoints swallows conversion
// errors into an empty result — safe for its callers (they treat "no
// endpoint" as "cannot verify, do nothing") but invisible — so a rendering
// regression here would silently disable those remediations rather than fail
// anything. This test is what fails instead.
//
// A real envtest apiserver backs the kube config because chart-state
// construction resolves cluster capabilities via API discovery.
func TestV2GetAdminAPIEndpoints(t *testing.T) {
	environment := &envtest.Environment{}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, environment.Stop())
	})

	m := &V2SimpleResourceRenderer{kubeConfig: config}

	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{Name: "endpoints", Namespace: "test-ns"},
	}

	addresses := m.GetAdminAPIEndpoints(NewClusterWithPools(rp))
	assert.Equal(t, []string{
		"endpoints-0.endpoints.test-ns.svc.cluster.local.:9644",
		"endpoints-1.endpoints.test-ns.svc.cluster.local.:9644",
		"endpoints-2.endpoints.test-ns.svc.cluster.local.:9644",
	}, addresses, "default cluster: one endpoint per replica (chart default 3), pod name as first label, default admin port")
}

// TestV2RenderSteersInternalService pins the per-cluster opt-in: only a
// cluster carrying feature.EndpointSteering has its internal Service -- the
// one its Schema Registry listener is on -- handed to the steering
// controller, and nothing else it renders is touched.
func TestV2RenderSteersInternalService(t *testing.T) {
	environment := &envtest.Environment{}
	config, err := environment.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, environment.Stop())
	})

	m := &V2SimpleResourceRenderer{kubeConfig: config}

	for _, steered := range []bool{false, true} {
		rp := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{Name: "steering", Namespace: "test-ns"},
		}
		if steered {
			rp.Annotations = map[string]string{feature.EndpointSteering.Key: "true"}
		}

		resources, err := m.Render(t.Context(), NewClusterWithPools(rp), "")
		require.NoError(t, err)

		var internal *corev1.Service
		for _, resource := range resources {
			svc, ok := resource.(*corev1.Service)
			if !ok {
				continue
			}
			if svc.Name == "steering" {
				internal = svc
				continue
			}
			assert.NotContains(t, svc.Annotations, endpointsteering.ServiceAnnotation, "service %q", svc.Name)
			assert.NotEmpty(t, svc.Spec.Selector, "service %q", svc.Name)
		}
		require.NotNil(t, internal, "the internal service is always rendered")

		if !steered {
			require.NotContains(t, internal.Annotations, endpointsteering.ServiceAnnotation)
			require.NotEmpty(t, internal.Spec.Selector)
			continue
		}
		require.Equal(t, "steering", internal.Annotations[endpointsteering.ServiceAnnotation])
		require.Nil(t, internal.Spec.Selector)
	}
}
