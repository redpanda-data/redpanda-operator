package console

import (
	"fmt"
	"os/exec"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/utils/ptr"
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
