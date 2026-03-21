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
					Enabled:  ptr.To(false),
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

// findHTTPRoute extracts the rendered HTTPRoute from a Render output, or nil.
func findHTTPRoute(objs []kube.Object) *gatewayv1.HTTPRoute {
	for _, obj := range objs {
		if !isNonNil(obj) {
			continue
		}
		if hr, ok := obj.(*gatewayv1.HTTPRoute); ok {
			return hr
		}
	}
	return nil
}

func TestGatewayConfigFields(t *testing.T) {
	t.Run("hostnames", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"a.example.com", "b.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Equal(t, []gatewayv1.Hostname{"a.example.com", "b.example.com"}, hr.Spec.Hostnames)
	})

	t.Run("change hostnames", func(t *testing.T) {
		// First config
		state1, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"old.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr1 := findHTTPRoute(Render(state1))
		require.NotNil(t, hr1)
		require.Equal(t, []gatewayv1.Hostname{"old.example.com"}, hr1.Spec.Hostnames)

		// Updated config
		state2, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"new.example.com", "also-new.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr2 := findHTTPRoute(Render(state2))
		require.NotNil(t, hr2)
		require.Equal(t, []gatewayv1.Hostname{"new.example.com", "also-new.example.com"}, hr2.Spec.Hostnames)
	})

	t.Run("path and pathType", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				Path:      ptr.To("/api/v1"),
				PathType:  ptr.To(gatewayv1.PathMatchExact),
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Len(t, hr.Spec.Rules, 1)
		require.Len(t, hr.Spec.Rules[0].Matches, 1)
		match := hr.Spec.Rules[0].Matches[0]
		require.NotNil(t, match.Path)
		require.Equal(t, gatewayv1.PathMatchExact, *match.Path.Type)
		require.Equal(t, "/api/v1", *match.Path.Value)
	})

	t.Run("change path", func(t *testing.T) {
		state1, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				Path:      ptr.To("/old"),
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr1 := findHTTPRoute(Render(state1))
		require.Equal(t, "/old", *hr1.Spec.Rules[0].Matches[0].Path.Value)

		state2, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				Path:      ptr.To("/new/path"),
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr2 := findHTTPRoute(Render(state2))
		require.Equal(t, "/new/path", *hr2.Spec.Rules[0].Matches[0].Path.Value)
	})

	t.Run("annotations", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				Annotations: map[string]string{
					"example.com/team":  "platform",
					"example.com/owner": "alice",
				},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw")},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Equal(t, "platform", hr.Annotations["example.com/team"])
		require.Equal(t, "alice", hr.Annotations["example.com/owner"])
	})

	t.Run("change annotations", func(t *testing.T) {
		state1, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:     ptr.To(true),
				Hostnames:   []string{"console.example.com"},
				Annotations: map[string]string{"old-key": "old-val"},
				ParentRefs:  []PartialGatewayParentReference{{Name: ptr.To("gw")}},
			},
		})
		require.NoError(t, err)
		hr1 := findHTTPRoute(Render(state1))
		require.Equal(t, "old-val", hr1.Annotations["old-key"])

		state2, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:     ptr.To(true),
				Hostnames:   []string{"console.example.com"},
				Annotations: map[string]string{"new-key": "new-val"},
				ParentRefs:  []PartialGatewayParentReference{{Name: ptr.To("gw")}},
			},
		})
		require.NoError(t, err)
		hr2 := findHTTPRoute(Render(state2))
		require.Equal(t, "new-val", hr2.Annotations["new-key"])
		require.Empty(t, hr2.Annotations["old-key"])
	})

	t.Run("parentRefs with all fields", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{
						Name:        ptr.To("primary-gw"),
						Namespace:   ptr.To("gateway-system"),
						SectionName: ptr.To(gatewayv1.SectionName("https")),
					},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Len(t, hr.Spec.ParentRefs, 1)
		ref := hr.Spec.ParentRefs[0]
		require.Equal(t, gatewayv1.ObjectName("primary-gw"), ref.Name)
		require.NotNil(t, ref.Namespace)
		require.Equal(t, gatewayv1.Namespace("gateway-system"), *ref.Namespace)
		require.NotNil(t, ref.SectionName)
		require.Equal(t, gatewayv1.SectionName("https"), *ref.SectionName)
	})

	t.Run("multiple parentRefs", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("gw-a"), Namespace: ptr.To("ns-a")},
					{Name: ptr.To("gw-b"), Namespace: ptr.To("ns-b"), SectionName: ptr.To(gatewayv1.SectionName("http"))},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Len(t, hr.Spec.ParentRefs, 2)
		require.Equal(t, gatewayv1.ObjectName("gw-a"), hr.Spec.ParentRefs[0].Name)
		require.Equal(t, gatewayv1.ObjectName("gw-b"), hr.Spec.ParentRefs[1].Name)
		require.Equal(t, gatewayv1.SectionName("http"), *hr.Spec.ParentRefs[1].SectionName)
	})

	t.Run("change parentRefs", func(t *testing.T) {
		state1, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:    ptr.To(true),
				Hostnames:  []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{{Name: ptr.To("old-gw")}},
			},
		})
		require.NoError(t, err)
		hr1 := findHTTPRoute(Render(state1))
		require.Equal(t, gatewayv1.ObjectName("old-gw"), hr1.Spec.ParentRefs[0].Name)

		state2, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("new-gw"), Namespace: ptr.To("new-ns")},
				},
			},
		})
		require.NoError(t, err)
		hr2 := findHTTPRoute(Render(state2))
		require.Equal(t, gatewayv1.ObjectName("new-gw"), hr2.Spec.ParentRefs[0].Name)
		require.Equal(t, gatewayv1.Namespace("new-ns"), *hr2.Spec.ParentRefs[0].Namespace)
	})

	t.Run("parentRef with only name", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:   ptr.To(true),
				Hostnames: []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{
					{Name: ptr.To("simple-gw")},
				},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.Len(t, hr.Spec.ParentRefs, 1)
		require.Equal(t, gatewayv1.ObjectName("simple-gw"), hr.Spec.ParentRefs[0].Name)
		require.Nil(t, hr.Spec.ParentRefs[0].Namespace)
		require.Nil(t, hr.Spec.ParentRefs[0].SectionName)
	})

	t.Run("default pathType is PathPrefix", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Gateway: &PartialGatewayConfig{
				Enabled:    ptr.To(true),
				Hostnames:  []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{{Name: ptr.To("gw")}},
				// PathType not set — should default to PathPrefix
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Equal(t, gatewayv1.PathMatchPathPrefix, *hr.Spec.Rules[0].Matches[0].Path.Type)
	})

	t.Run("backend service port from values", func(t *testing.T) {
		state, err := NewRenderState("ns", "rel", nil, PartialRenderValues{
			Service: &PartialServiceConfig{
				Port: ptr.To(int32(9090)),
			},
			Gateway: &PartialGatewayConfig{
				Enabled:    ptr.To(true),
				Hostnames:  []string{"console.example.com"},
				ParentRefs: []PartialGatewayParentReference{{Name: ptr.To("gw")}},
			},
		})
		require.NoError(t, err)
		hr := findHTTPRoute(Render(state))
		require.NotNil(t, hr)
		require.Len(t, hr.Spec.Rules[0].BackendRefs, 1)
		require.Equal(t, gatewayv1.PortNumber(9090), *hr.Spec.Rules[0].BackendRefs[0].Port)
	})
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
