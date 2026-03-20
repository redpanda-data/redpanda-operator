// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"
	"os/exec"
	"reflect"
	"testing"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TestAppVersion asserts that the AppVersion const is inline with the version
// of console that's used for generating PartialConfig. In practice, it's
// acceptable for there to be a bit of difference as the config is fairly
// stable but that assertion is much harder to write.
func TestAppVersion(t *testing.T) {
	const (
		gitCmd = "git ls-remote https://github.com/redpanda-data/console.git %s | cut -c 1-12"
		goCmd  = "go list -m -json github.com/redpanda-data/console/backend | jq -r .Version | cut -d - -f 3"
	)

	gitOut, err := exec.Command("sh", "-c", fmt.Sprintf(gitCmd, AppVersion)).CombinedOutput()
	require.NoError(t, err)

	goOut, err := exec.Command("sh", "-c", goCmd).CombinedOutput()
	require.NoError(t, err)

	require.Equal(t, string(gitOut), string(goOut), ".AppVersion and go.mod should refer to the same version of console:\nAppVersion: %s\ngo.mod: %sgit: %s", AppVersion, goOut, gitOut)
}

func TestTypes(t *testing.T) {
	// Build a map of allowed types from Types() function
	allowableTypes := map[reflect.Type]struct{}{}
	for _, typ := range Types() {
		allowableTypes[reflect.TypeOf(typ)] = struct{}{}
	}

	testCases := []struct {
		name   string
		values PartialRenderValues
	}{
		{
			name:   "minimal config",
			values: PartialRenderValues{},
		},
		{
			name: "all features enabled",
			values: PartialRenderValues{
				ServiceAccount: &PartialServiceAccountConfig{
					Create: ptr.To(true),
				},
				Secret: &PartialSecretConfig{
					Create: ptr.To(true),
				},
				ConfigMap: &PartialCreatable{
					Create: ptr.To(true),
				},
				Deployment: &PartialDeploymentConfig{
					Create: ptr.To(true),
				},
				Gateway: &PartialGatewayConfig{
					Enabled: ptr.To(true),
					ParentRefs: []PartialGatewayParentReference{
						{
							Name: ptr.To("public-gateway"),
						},
					},
					Hostnames: []string{"console.example.com"},
				},
				Autoscaling: &PartialAutoScaling{
					Enabled:     ptr.To(true),
					MinReplicas: ptr.To(int32(1)),
					MaxReplicas: ptr.To(int32(3)),
				},
			},
		},
		{
			name: "service account disabled",
			values: PartialRenderValues{
				ServiceAccount: &PartialServiceAccountConfig{
					Create: ptr.To(false),
				},
				ConfigMap: &PartialCreatable{
					Create: ptr.To(true),
				},
			},
		},
		{
			name: "ingress disabled",
			values: PartialRenderValues{
				Ingress: &PartialIngressConfig{
					Enabled: ptr.To(false),
				},
				ConfigMap: &PartialCreatable{
					Create: ptr.To(true),
				},
			},
		},
		{
			name: "gateway disabled",
			values: PartialRenderValues{
				Gateway: &PartialGatewayConfig{
					Enabled: ptr.To(false),
					PathType: ptr.To(gatewayv1.PathMatchPathPrefix),
				},
				ConfigMap: &PartialCreatable{
					Create: ptr.To(true),
				},
			},
		},
		{
			name: "autoscaling disabled",
			values: PartialRenderValues{
				Autoscaling: &PartialAutoScaling{
					Enabled: ptr.To(false),
				},
				ConfigMap: &PartialCreatable{
					Create: ptr.To(true),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := NewRenderState("test-namespace", "test-release", nil, tc.values)
			require.NoError(t, err)

			for _, obj := range Render(state) {
				objType := reflect.TypeOf(obj)
				_, ok := allowableTypes[objType]
				require.True(t, ok, "%T is not an allowable type. Did you forget to update `console.Types`?", obj)
			}
		})
	}
}

func TestIngressGatewayMutualExclusion(t *testing.T) {
	_, err := NewRenderState("test-namespace", "test-release", nil, PartialRenderValues{
		Ingress: &PartialIngressConfig{
			Enabled: ptr.To(true),
			Hosts: []PartialIngressHost{
				{
					Host: ptr.To("console.example.com"),
					Paths: []PartialIngressPath{
						{
							Path:     ptr.To("/"),
							PathType: ptr.To(networkingv1.PathTypePrefix),
						},
					},
				},
			},
		},
		Gateway: &PartialGatewayConfig{
			Enabled: ptr.To(true),
			ParentRefs: []PartialGatewayParentReference{
				{
					Name: ptr.To("public-gateway"),
				},
			},
			Hostnames: []string{"console.example.com"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ingress and gateway cannot both be enabled")
}

// isNonNil returns true if the kube.Object interface holds a non-nil pointer.
func isNonNil(obj kube.Object) bool {
	return obj != nil && !reflect.ValueOf(obj).IsNil()
}

func TestGatewayRemoval(t *testing.T) {
	// Simulate the scenario where gateway was previously enabled and is now
	// removed from the config. Console should render without errors and
	// produce no HTTPRoute.
	t.Run("gateway removed from config", func(t *testing.T) {
		state, err := NewRenderState("test-namespace", "test-release", nil, PartialRenderValues{
			ConfigMap: &PartialCreatable{
				Create: ptr.To(true),
			},
			// No gateway stanza at all - simulates removal
		})
		require.NoError(t, err)

		for _, obj := range Render(state) {
			if !isNonNil(obj) {
				continue
			}
			_, isHTTPRoute := obj.(*gatewayv1.HTTPRoute)
			require.False(t, isHTTPRoute, "HTTPRoute should not be rendered when gateway is not configured")
		}
	})

	t.Run("gateway explicitly disabled", func(t *testing.T) {
		state, err := NewRenderState("test-namespace", "test-release", nil, PartialRenderValues{
			ConfigMap: &PartialCreatable{
				Create: ptr.To(true),
			},
			Gateway: &PartialGatewayConfig{
				Enabled: ptr.To(false),
			},
		})
		require.NoError(t, err)

		for _, obj := range Render(state) {
			if !isNonNil(obj) {
				continue
			}
			_, isHTTPRoute := obj.(*gatewayv1.HTTPRoute)
			require.False(t, isHTTPRoute, "HTTPRoute should not be rendered when gateway is disabled")
		}
	})

	t.Run("switch from gateway to ingress", func(t *testing.T) {
		// First render with gateway
		state1, err := NewRenderState("test-namespace", "test-release", nil, PartialRenderValues{
			ConfigMap: &PartialCreatable{Create: ptr.To(true)},
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("my-gw")},
				},
			},
		})
		require.NoError(t, err)

		var hasHTTPRoute bool
		for _, obj := range Render(state1) {
			if !isNonNil(obj) {
				continue
			}
			if _, ok := obj.(*gatewayv1.HTTPRoute); ok {
				hasHTTPRoute = true
			}
		}
		require.True(t, hasHTTPRoute, "first render should have HTTPRoute")

		// Now render with ingress instead (gateway removed)
		state2, err := NewRenderState("test-namespace", "test-release", nil, PartialRenderValues{
			ConfigMap: &PartialCreatable{Create: ptr.To(true)},
			Ingress: &PartialIngressConfig{
				Enabled: ptr.To(true),
				Hosts: []PartialIngressHost{
					{
						Host:  ptr.To("console.example.com"),
						Paths: []PartialIngressPath{{Path: ptr.To("/"), PathType: ptr.To(networkingv1.PathTypePrefix)}},
					},
				},
			},
		})
		require.NoError(t, err)

		var hasIngress bool
		hasHTTPRoute = false
		for _, obj := range Render(state2) {
			if !isNonNil(obj) {
				continue
			}
			if _, ok := obj.(*gatewayv1.HTTPRoute); ok {
				hasHTTPRoute = true
			}
			if _, ok := obj.(*networkingv1.Ingress); ok {
				hasIngress = true
			}
		}
		require.False(t, hasHTTPRoute, "second render should not have HTTPRoute")
		require.True(t, hasIngress, "second render should have Ingress")
	})
}
