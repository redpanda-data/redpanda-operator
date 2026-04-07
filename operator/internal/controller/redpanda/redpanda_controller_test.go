// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/otelutil/log"
	"github.com/redpanda-data/common-go/otelutil/trace"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/lifecycle"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/feature"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// operatorRBAC is the ClusterRole and Role generated via controller-gen and
// goembeded so it can be used for tests.
//
//go:embed testdata/role.yaml
var operatorRBAC []byte

// NB: This test setup is largely incompatible with webhooks. Though we might
// be able to figure something freaky out.
func TestIntegrationRedpandaController(t *testing.T) {
	testutil.SkipIfNotIntegration(t)
	suite.Run(t, new(RedpandaControllerSuite))
}

type RedpandaControllerSuite struct {
	suite.Suite

	env           *testenv.Env
	clientFactory internalclient.ClientFactory
}

var _ suite.SetupAllSuite = (*RedpandaControllerSuite)(nil)

func (s *RedpandaControllerSuite) setup() (*testing.T, context.Context, context.CancelFunc, client.Client) {
	t := s.T()
	t.Parallel()
	ctx, cancel := context.WithTimeout(trace.Test(t), 15*time.Minute)
	ns := s.env.CreateTestNamespace(t)
	return t, ctx, cancel, ns.Client
}

func (s *RedpandaControllerSuite) TestManaged() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()

	s.applyAndWait(t, ctx, c, rp)

	// We've had default feature annotations applied.
	require.Equal(t, "true", rp.Annotations[feature.V2Managed.Key])

	rp.Annotations[feature.V2Managed.Key] = "false"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(10) // Better hope this feature works.

	// The signifier that this a cluster is not being "managed" any more is
	// that its finalizes have been removed.
	s.applyAndWaitFor(t, ctx, c, func(o client.Object, err error) (bool, error) {
		return len(o.(*redpandav1alpha2.Redpanda).Finalizers) == 0, nil
	})

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestObjectsGCed() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()

	// NB: this test originally tested GC behavior through Console deployments
	// now that Console stanzas are migrated to their own CRD which we intentionally
	// orphan, we need to test this with some other resource that gets GC'd, here
	// namely an external service.
	rp.Spec.ClusterSpec.External.Enabled = ptr.To(true)
	rp.Spec.ClusterSpec.External.Service = &redpandav1alpha2.ExternalService{
		Enabled: ptr.To(true),
	}
	rp.Spec.ClusterSpec.External.Type = ptr.To(string(corev1.ServiceTypeLoadBalancer))
	for range *rp.Spec.ClusterSpec.Statefulset.Replicas {
		rp.Spec.ClusterSpec.External.Addresses = append(rp.Spec.ClusterSpec.External.Addresses, "127.0.0.1:1234")
	}

	s.applyAndWait(t, ctx, c, rp)

	// Create a list of secrets with varying labels that we expect to NOT get
	// GC'd.
	secrets := []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"helm.toolkit.fluxcd.io/name":      rp.Name,
					"helm.toolkit.fluxcd.io/namespace": rp.Namespace,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"helm.toolkit.fluxcd.io/name":      rp.Name,
					"helm.toolkit.fluxcd.io/namespace": rp.Namespace,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "no-gc-",
				Labels: map[string]string{
					"app.kubernetes.io/instance": rp.Name,
				},
			},
		},
	}

	for _, secret := range secrets {
		require.NoError(t, c.Create(ctx, secret))
	}

	// Assert that the external service exists
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		var services corev1.ServiceList
		assert.NoError(t, c.List(ctx, &services, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name, "app.kubernetes.io/name": "redpanda"}))
		found := false
		for _, service := range services.Items {
			if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
				found = true
			}
		}
		assert.True(t, found)
	}, time.Minute, time.Second, "external service not found")

	rp.Spec.ClusterSpec.External.Enabled = ptr.To(false)
	s.applyAndWait(t, ctx, c, rp)

	// Assert that the external service has been garbage collected.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		var services corev1.ServiceList
		assert.NoError(t, c.List(ctx, &services, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name, "app.kubernetes.io/name": "redpanda"}))
		found := false
		for _, service := range services.Items {
			if service.Spec.Type == corev1.ServiceTypeLoadBalancer {
				found = true
			}
		}
		assert.False(t, found)
	}, time.Minute, time.Second, "external service not GC'd")

	// Assert that our previously created secrets have not been GC'd.
	for _, secret := range secrets {
		key := client.ObjectKeyFromObject(secret)
		require.NoError(t, c.Get(ctx, key, secret))
	}

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestTPLValues() {
	t, ctx, cancel, c := s.setup() //nolint:staticcheck // ctx and c used after skip is removed
	defer cancel()

	t.Skip("invalid / broken due to changes in chart v25.1.1 (podTemplate)")
	_ = ctx
	_ = c

	rp := s.minimalRP()

	extraVolumeMount := ptr.To(`- name: test-extra-volume
  mountPath: {{ upper "/fake/lifecycle" }}`)

	rp.Spec.ClusterSpec.Statefulset.ExtraVolumeMounts = extraVolumeMount
	rp.Spec.ClusterSpec.Statefulset.ExtraVolumes = ptr.To(`- name: test-extra-volume
  secret:
    secretName: {{ (get (fromJson (include "redpanda.Fullname" (dict "a" (list .)))) "r") }}-sts-lifecycle
    defaultMode: 0774
`)
	rp.Spec.ClusterSpec.Statefulset.InitContainers = &redpandav1alpha2.InitContainers{
		Configurator:                      &redpandav1alpha2.Configurator{ExtraVolumeMounts: extraVolumeMount},
		FsValidator:                       &redpandav1alpha2.FsValidator{ExtraVolumeMounts: extraVolumeMount},
		Tuning:                            &redpandav1alpha2.Tuning{ExtraVolumeMounts: extraVolumeMount},
		SetDataDirOwnership:               &redpandav1alpha2.SetDataDirOwnership{ExtraVolumeMounts: extraVolumeMount},
		SetTieredStorageCacheDirOwnership: &redpandav1alpha2.SetTieredStorageCacheDirOwnership{ExtraVolumeMounts: extraVolumeMount},
		ExtraInitContainers: ptr.To(`- name: "test-init-container"
  image: "alpine:latest"
  command: [ "/bin/sh", "-c" ]
  volumeMounts:
  - name: test-extra-volume
    mountPath: /FAKE/LIFECYCLE
  args:
    - |
      set -xe
      echo "Hello World!"`),
	}
	rp.Spec.ClusterSpec.Statefulset.SideCars = &redpandav1alpha2.SideCars{ConfigWatcher: &redpandav1alpha2.ConfigWatcher{ExtraVolumeMounts: extraVolumeMount}}
	s.applyAndWait(t, ctx, c, rp)

	var sts appsv1.StatefulSet
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: rp.Name, Namespace: rp.Namespace}, &sts))

	require.Contains(t, sts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "test-extra-volume",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{DefaultMode: ptr.To(int32(508)), SecretName: fmt.Sprintf("%s-sts-lifecycle", rp.Name)},
		},
	})
	for _, cont := range sts.Spec.Template.Spec.InitContainers {
		if cont.Name == "bootstrap-yaml-envsubst" {
			continue
		}

		require.Contains(t, cont.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})

		if cont.Name == "test-init-container" {
			require.Equal(t, cont.Command, []string{"/bin/sh", "-c"})
			require.Equal(t, cont.Args, []string{"set -xe\necho \"Hello World!\""})
			require.Equal(t, cont.Image, "alpine:latest")
		}
	}
	for _, cont := range sts.Spec.Template.Spec.Containers {
		require.Contains(t, cont.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})
	}

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestExternalSecretInjection() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Config.ExtraClusterConfiguration = map[string]vectorizedv1alpha1.ClusterConfigValue{
		"segment_appender_flush_timeout_ms": {
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "injected-value",
				},
				Key: "value-1",
			},
			UseRawValue: true,
		},
	}

	t.Log("Applying secret injected-value")
	s.applyAndWait(t, ctx, c, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "injected-value",
		},
		Data: map[string]string{
			"value-1": "1003",
		},
	})
	s.applyAndWait(t, ctx, c, rp)

	adminClient, err := s.clientFactory.RedpandaAdminClient(ctx, rp)
	require.NoError(t, err)
	config, err := adminClient.Config(ctx, false)
	require.NoError(t, err)

	require.Equal(t, float64(1003), config["segment_appender_flush_timeout_ms"])
}

func (s *RedpandaControllerSuite) TestClusterSettings() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[feature.RestartOnConfigChange.Key] = "true"

	// Ensure that some superusers exist.
	rp.Spec.ClusterSpec.Auth = &redpandav1alpha2.Auth{
		SASL: &redpandav1alpha2.SASL{
			Enabled: ptr.To(true),
			Users: []redpandav1alpha2.UsersItems{
				{Name: ptr.To("bob"), Password: ptr.To("bobert")},
				{Name: ptr.To("alice"), Password: ptr.To("alicert")},
			},
		},
	}

	rp.Spec.ClusterSpec.Storage = &redpandav1alpha2.Storage{
		Tiered: &redpandav1alpha2.Tiered{
			Config: &redpandav1alpha2.TieredConfig{
				CloudStorageDisableTLS: ptr.To(true),
			},
			CredentialsSecretRef: &redpandav1alpha2.CredentialSecretRef{
				AccessKey: &redpandav1alpha2.SecretWithConfigField{
					Name: ptr.To("creds"),
					Key:  ptr.To("access_key"),
				},
			},
		},
	}
	s.applyAndWait(t, ctx, c, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creds",
		},
		Data: map[string][]byte{
			"access_key": []byte("VURYSECRET"),
		},
	})
	s.applyAndWait(t, ctx, c, rp)

	setConfig := func(cfg map[string]any) func() {
		asJSON, err := json.Marshal(cfg)
		require.NoError(t, err)

		rp.Spec.ClusterSpec.Config.Cluster = &runtime.RawExtension{Raw: asJSON}
		s.apply(t, ctx, c, rp)
		return func() {
			s.waitUntilReady(t, ctx, c, rp)
			s.waitFor(t, ctx, c, rp, func(o client.Object, err error) (bool, error) {
				if err != nil {
					return false, err
				}
				rp := o.(*redpandav1alpha2.Redpanda)
				for _, cond := range rp.Status.Conditions {
					if cond.Type == statuses.ClusterConfigurationApplied {
						return cond.ObservedGeneration == rp.Generation && cond.Status == metav1.ConditionTrue, nil
					}
				}
				return false, nil
			})
		}
	}

	cases := []struct {
		Name     string
		In       map[string]any
		Expected map[string]any
	}{
		{
			Name: "should_create_superusers",
			In: map[string]any{
				"admin_api_require_auth":      true,
				"enable_schema_id_validation": "redpanda",
				"enable_transactions":         false,
			},
			Expected: map[string]any{
				"admin_api_require_auth":      true,
				"cloud_storage_access_key":    "VURYSECRET",
				"cloud_storage_disable_tls":   true,
				"enable_schema_id_validation": "redpanda",
				"enable_transactions":         false,
				"superusers":                  []any{"alice", "bob", "kubernetes-controller"},
			},
		},
		{
			Name: "should_enable_transactions",
			In: map[string]any{
				"enable_transactions":         true,
				"enable_schema_id_validation": "none",
				"superusers":                  []any{"jimbob"},
			},
			Expected: map[string]any{
				"admin_api_require_auth":    true,
				"cloud_storage_access_key":  "VURYSECRET",
				"cloud_storage_disable_tls": true,
				"superusers":                []any{"alice", "bob", "jimbob", "kubernetes-controller"},
			},
		},
		{
			// Adding a test case with data_transforms_enabled which requires a
			// cluster restart, to validate that the operator ensures the cluster
			// is restarted.
			Name: "should_enable_transforms",
			In: map[string]any{
				"data_transforms_enabled": true, // needs restart
			},
			Expected: map[string]any{
				"data_transforms_enabled": true,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(_ *testing.T) {
			s.waitUntilReady(t, ctx, c, rp)

			adminClient, err := s.clientFactory.RedpandaAdminClient(ctx, rp)
			require.NoError(t, err)
			defer adminClient.Close()

			var initialVersion int64
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				st, err := adminClient.ClusterConfigStatus(ctx, false)
				if !assert.NoError(ct, err) {
					t.Logf("[%s] getting cluster config status failed with: %v", time.Now().Format(time.DateTime), err)
					return
				}
				initialVersion = slices.MaxFunc(st, func(a, b rpadmin.ConfigStatus) int {
					return int(a.ConfigVersion - b.ConfigVersion)
				}).ConfigVersion
			}, 5*time.Minute, time.Second)

			waitFn := setConfig(tc.In)
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				st, err := adminClient.ClusterConfigStatus(ctx, false)
				if !assert.NoError(ct, err) {
					t.Logf("[%s] getting cluster config status failed with: %v", time.Now().Format(time.DateTime), err)
					return
				}
				t.Logf("[%s] cluster status: %v", time.Now().Format(time.DateTime), st)
				currVersion := slices.MinFunc(st, func(a, b rpadmin.ConfigStatus) int {
					return int(a.ConfigVersion - b.ConfigVersion)
				}).ConfigVersion

				// Only operator should change cluster configuration once. If there is any other party that changes
				// Redpanda cluster configuration, it is unexpected and should be investigated.
				assert.Equal(ct, initialVersion+1, currVersion, "current config version should increase only by one")

				assert.False(ct, slices.ContainsFunc(st, func(cs rpadmin.ConfigStatus) bool {
					return cs.Restart
				}), "expected no brokers to need restart")
			}, 5*time.Minute, time.Second)
			// wait for the cluster to be ready and the configuration synced
			waitFn()

			config, err := adminClient.Config(ctx, false)
			require.NoError(t, err)

			arr := config["superusers"].([]any)

			sort.Slice(arr, func(i, j int) bool {
				return arr[i].(string) < arr[j].(string)
			})

			// Only assert that tc.Expected is a subset of the set config.
			// The chart/operator injects a bunch of "useful" values by default.
			require.Subset(t, config, tc.Expected)
		})
	}

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestClusterSettingsRegressionSuperusers() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	rp := s.minimalRP()
	rp.Annotations[feature.RestartOnConfigChange.Key] = "true"

	// Ensure that some superusers exist.
	rp.Spec.ClusterSpec.Auth = &redpandav1alpha2.Auth{
		SASL: &redpandav1alpha2.SASL{
			Enabled: ptr.To(true),
			Users: []redpandav1alpha2.UsersItems{
				{Name: ptr.To("bob"), Password: ptr.To("bobert")},
				{Name: ptr.To("alice"), Password: ptr.To("alicert")},
			},
		},
	}

	s.applyAndWait(t, ctx, c, rp)

	initialVersion := rp.Status.ConfigVersion

	// Add a superuser and ensure we don't change the config version (since we now hash).
	rp.Spec.ClusterSpec.Auth = &redpandav1alpha2.Auth{
		SASL: &redpandav1alpha2.SASL{
			Enabled: ptr.To(true),
			Users: []redpandav1alpha2.UsersItems{
				{Name: ptr.To("bob"), Password: ptr.To("bobert")},
				{Name: ptr.To("alice"), Password: ptr.To("alicert")},
				{Name: ptr.To("ted"), Password: ptr.To("tedrt")},
			},
		},
	}

	s.applyAndWait(t, ctx, c, rp)

	require.Equal(t, initialVersion, rp.Status.ConfigVersion)

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestLicenseReal() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	const (
		LicenseEnvVar             = "REDPANDA_SAMPLE_LICENSE"
		RedpandaLicenseSecretName = "rp-license"
		RedpandaLicenseKeyName    = "license"
	)
	license := os.Getenv(LicenseEnvVar)
	if license == "" {
		t.Skipf("License environment variable %q is not provided", LicenseEnvVar)
	}

	s.apply(t, ctx, c, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: RedpandaLicenseSecretName,
		},
		Data: map[string][]byte{
			RedpandaLicenseKeyName: []byte(license),
		},
	})

	redpandas := map[string]*redpandav1alpha2.Redpanda{}

	rp := s.minimalRP()
	rp.ObjectMeta.GenerateName = ""
	rp.SetName("literal-license")
	rp.Spec.ClusterSpec.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{
		Spec: &applycorev1.PodSpecApplyConfiguration{
			Containers: []applycorev1.ContainerApplyConfiguration{{
				Name: ptr.To("redpanda"),
				Env: []applycorev1.EnvVarApplyConfiguration{
					*applycorev1.EnvVar().WithName("__REDPANDA_DISABLE_BUILTIN_TRIAL_LICENSE").WithValue("true"),
				},
			}},
		},
	}
	rp.Spec.ClusterSpec.Enterprise = &redpandav1alpha2.Enterprise{
		License: &license,
	}

	rp.Spec.ClusterSpec.Config.Cluster = convertToRawExtension(t, map[string]bool{
		"core_balancing_continuous": true,
	})

	redpandas["literal-license"] = rp

	rp = rp.DeepCopy()
	rp.SetName("license-in-secret")
	rp.Spec.ClusterSpec.Enterprise = &redpandav1alpha2.Enterprise{
		License: nil,
		LicenseSecretRef: &redpandav1alpha2.EnterpriseLicenseSecretRef{
			Key:  ptr.To(RedpandaLicenseKeyName),
			Name: ptr.To(RedpandaLicenseSecretName),
		},
	}

	redpandas["license-in-secret"] = rp

	for testCaseName, tc := range redpandas {
		tc := tc
		t.Run(testCaseName, func(t *testing.T) {
			var licenseStatus *redpandav1alpha2.RedpandaLicenseStatus
			s.applyAndWaitFor(t, ctx, c, func(o client.Object, err error) (bool, error) {
				if err != nil {
					return false, err
				}
				rp := o.(*redpandav1alpha2.Redpanda)

				for _, cond := range rp.Status.Conditions {
					if cond.Type == redpandav1alpha2.ClusterLicenseValid {
						// grab the first non-unknown status
						if cond.Status != metav1.ConditionUnknown {
							licenseStatus = rp.Status.LicenseStatus
							return true, nil
						}
						return false, nil
					}
				}
				return false, nil
			}, tc)

			sort.Strings(licenseStatus.InUseFeatures)

			require.Equal(t, &redpandav1alpha2.RedpandaLicenseStatus{
				Violation: false,
				InUseFeatures: []string{
					"core_balancing_continuous",
					"partition_auto_balancing_continuous",
				},
				Expired:      ptr.To(false),
				Type:         ptr.To("enterprise"),
				Organization: ptr.To("redpanda-testing"),
				Expiration:   licenseStatus.Expiration,
			}, licenseStatus)

			s.deleteAndWait(t, ctx, c, tc)
		})
	}
}

func (s *RedpandaControllerSuite) TestLicense() {
	t := s.T()
	t.Parallel()

	type image struct {
		repository string
		tag        string
	}

	cases := []struct {
		image                 image
		license               bool
		expected              string
		expectedLicenseStatus *redpandav1alpha2.RedpandaLicenseStatus
	}{{
		image: image{
			repository: "redpandadata/redpanda-unstable",
			tag:        "v24.3.1-rc4",
		},
		license:  false,
		expected: "Cluster license has expired",
		expectedLicenseStatus: &redpandav1alpha2.RedpandaLicenseStatus{
			Violation:     false,
			InUseFeatures: []string{},
			Expired:       ptr.To(true),
			Type:          ptr.To("free_trial"),
			Organization:  ptr.To("Redpanda Built-In Evaluation Period"),
		},
	}, {
		image: image{
			repository: "redpandadata/redpanda-unstable",
			tag:        "v24.3.1-rc8",
		},
		license:  true,
		expected: "Cluster has a valid license",
		expectedLicenseStatus: &redpandav1alpha2.RedpandaLicenseStatus{
			Violation:     false,
			InUseFeatures: []string{},
			Expired:       ptr.To(false),
			// add a 30 day expiration, which is how we handle trial licenses
			Expiration:   &metav1.Time{Time: time.Now().Add(30 * 24 * time.Hour).UTC()},
			Type:         ptr.To("free_trial"),
			Organization: ptr.To("Redpanda Built-In Evaluation Period"),
		},
	}, {
		image: image{
			repository: "redpandadata/redpanda",
			tag:        "v24.2.9",
		},
		license:  false,
		expected: "No cluster license is present",
		expectedLicenseStatus: &redpandav1alpha2.RedpandaLicenseStatus{
			Violation:     false,
			InUseFeatures: []string{},
		},
	}, {
		image: image{
			repository: "redpandadata/redpanda",
			tag:        "v24.2.9",
		},
		license:  true,
		expected: "No cluster license is present",
		expectedLicenseStatus: &redpandav1alpha2.RedpandaLicenseStatus{
			Violation:     false,
			InUseFeatures: []string{},
		},
	}}

	for _, tc := range cases {
		name := fmt.Sprintf("%s/%s (license: %t)", tc.image.repository, tc.image.tag, tc.license)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(trace.Test(t), 15*time.Minute)
			defer cancel()

			ns := s.env.CreateTestNamespace(t)
			c := ns.Client

			rp := s.minimalRP()
			rp.Spec.ClusterSpec.Image = &redpandav1alpha2.RedpandaImage{
				Repository: ptr.To(tc.image.repository),
				Tag:        ptr.To(tc.image.tag),
			}
			if !tc.license {
				rp.Spec.ClusterSpec.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{
					Spec: &applycorev1.PodSpecApplyConfiguration{
						Containers: []applycorev1.ContainerApplyConfiguration{{
							Name: ptr.To("redpanda"),
							Env: []applycorev1.EnvVarApplyConfiguration{
								*applycorev1.EnvVar().WithName("__REDPANDA_DISABLE_BUILTIN_TRIAL_LICENSE").WithValue("true"),
							},
						}},
					},
				}
			}

			var condition metav1.Condition
			var licenseStatus *redpandav1alpha2.RedpandaLicenseStatus
			s.applyAndWaitFor(t, ctx, c, func(o client.Object, err error) (bool, error) {
				if err != nil {
					return false, err
				}
				rp := o.(*redpandav1alpha2.Redpanda)

				for _, cond := range rp.Status.Conditions {
					if cond.Type == statuses.ClusterLicenseValid {
						// grab the first non-unknown status
						if cond.Status != metav1.ConditionUnknown {
							condition = cond
							licenseStatus = rp.Status.LicenseStatus
							return true, nil
						}
						return false, nil
					}
				}
				return false, nil
			}, rp)

			message := fmt.Sprintf("%s - %s != %s", name, tc.expected, condition.Message)
			require.Equal(t, tc.expected, condition.Message, message)

			if tc.expectedLicenseStatus == nil && licenseStatus != nil {
				t.Fatalf("%s does not have a nil license %s", name, licenseStatus.String())
			}

			if tc.expectedLicenseStatus != nil {
				require.NotNil(t, licenseStatus, "%s does has a nil license", name)
				require.Equal(t, licenseStatus.Expired, tc.expectedLicenseStatus.Expired, "%s license expired field does not match", name)
				require.EqualValues(t, licenseStatus.InUseFeatures, tc.expectedLicenseStatus.InUseFeatures, "%s license valid features do not match", name)
				require.Equal(t, licenseStatus.Organization, tc.expectedLicenseStatus.Organization, "%s license organization field does not match", name)
				require.Equal(t, licenseStatus.Type, tc.expectedLicenseStatus.Type, "%s license type field does not match", name)
				require.Equal(t, licenseStatus.Violation, tc.expectedLicenseStatus.Violation, "%s license violation field does not match", name)

				// only do the expiration check if the license isn't already expired
				if licenseStatus.Expired != nil && !*licenseStatus.Expired {
					expectedExpiration := tc.expectedLicenseStatus.Expiration.UTC()
					actualExpiration := licenseStatus.Expiration.UTC()

					rangeFactor := 5 * time.Minute
					// add some fudge factor so that we don't fail with flakiness due to tests being run at
					// the change of a couple of minutes that causes the date to be rolled over by some factor
					if !(expectedExpiration.Add(rangeFactor).After(actualExpiration) &&
						expectedExpiration.Add(-rangeFactor).Before(actualExpiration)) {
						t.Fatalf("%s does not match expected expiration: %s != %s", name, actualExpiration, expectedExpiration)
					}
				}
			}

			s.deleteAndWait(t, ctx, c, rp)
		})
	}
}

func (s *RedpandaControllerSuite) TestScaling() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	isStable := func(obj client.Object, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		rp := obj.(*redpandav1alpha2.Redpanda)
		for _, cond := range rp.Status.Conditions {
			if cond.Type == statuses.ClusterStable {
				return cond.ObservedGeneration == rp.Generation && cond.Status == metav1.ConditionTrue, nil
			}
		}
		return false, nil
	}

	rp := s.minimalRP()

	// Start with 5 brokers.
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(5)
	s.applyAndWaitFor(t, ctx, c, isStable, rp)

	// Scale down to 3.
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWaitFor(t, ctx, c, isStable, rp)

	// And then back up to 5.
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(5)
	s.applyAndWaitFor(t, ctx, c, isStable, rp)

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) TestNodePools() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	isStable := func(checkStable bool) func(obj client.Object, err error) (bool, error) {
		return func(obj client.Object, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			var conditions []metav1.Condition
			var stable string
			switch typ := obj.(type) {
			case *redpandav1alpha2.Redpanda:
				conditions = typ.Status.Conditions
				stable = statuses.ClusterStable
			case *redpandav1alpha2.NodePool:
				conditions = typ.Status.Conditions
				stable = statuses.NodePoolStable
			default:
				return false, errors.New("unsupported type")
			}
			status := metav1.ConditionTrue
			if !checkStable {
				status = metav1.ConditionFalse
			}
			for _, cond := range conditions {
				if cond.Type == stable {
					return cond.ObservedGeneration == obj.GetGeneration() && cond.Status == status, nil
				}
			}
			return false, nil
		}
	}

	rp := s.minimalRP()

	// start with one broker and no nodepools
	s.applyAndWaitFor(t, ctx, c, isStable(true), rp)

	// add another broker via a nodepool.
	pool := s.minimalNodePool(rp)
	s.applyAndWaitFor(t, ctx, c, isStable(true), pool)

	s.deleteAndWait(t, ctx, c, rp)

	// we should go unstable due to an unhealthy broker
	s.waitFor(t, ctx, c, pool, isStable(false))
	// and eventually we should fully unbind
	s.waitFor(t, ctx, c, pool, func(o client.Object, err error) (bool, error) {
		if err != nil {
			return false, err
		}
		for _, cond := range o.(*redpandav1alpha2.NodePool).Status.Conditions {
			if cond.Type == statuses.NodePoolBound {
				return cond.ObservedGeneration == o.GetGeneration() && cond.Status == metav1.ConditionFalse, nil
			}
		}
		return false, nil
	})
	s.deleteAndWait(t, ctx, c, pool)
}

func (s *RedpandaControllerSuite) TestNodePoolsBlueGreen() {
	t, ctx, cancel, c := s.setup()
	defer cancel()

	isStable := func(checkStable bool) func(obj client.Object, err error) (bool, error) {
		return func(obj client.Object, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			var conditions []metav1.Condition
			var stable string
			switch typ := obj.(type) {
			case *redpandav1alpha2.Redpanda:
				conditions = typ.Status.Conditions
				stable = statuses.ClusterStable
			case *redpandav1alpha2.NodePool:
				conditions = typ.Status.Conditions
				stable = statuses.NodePoolStable
			default:
				return false, errors.New("unsupported type")
			}
			status := metav1.ConditionTrue
			if !checkStable {
				status = metav1.ConditionFalse
			}
			for _, cond := range conditions {
				if cond.Type == stable {
					return cond.ObservedGeneration == obj.GetGeneration() && cond.Status == status, nil
				}
			}
			return false, nil
		}
	}

	rp := s.minimalRP()

	// set the default pool's replicas to 0
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(0)

	// start with no brokers and no nodepools
	// we don't wait for stability here because the cluster
	// won't stabilize as it has 0 brokers.
	s.apply(t, ctx, c, rp)

	// add a nodepool with 3 brokers.
	bluePool := s.minimalNodePool(rp)
	bluePool.Spec.Replicas = ptr.To(int32(3))
	s.applyAndWaitFor(t, ctx, c, isStable(true), bluePool)
	// now we make sure the cluster is stable each modification
	s.waitFor(t, ctx, c, rp, isStable(true))

	greenPool := s.minimalNodePool(rp)
	greenPool.Spec.Replicas = ptr.To(int32(0))
	s.applyAndWaitFor(t, ctx, c, isStable(true), greenPool)
	s.waitFor(t, ctx, c, rp, isStable(true))

	greenPool.Spec.Replicas = ptr.To(int32(3))
	s.applyAndWaitFor(t, ctx, c, isStable(true), greenPool)
	s.waitFor(t, ctx, c, rp, isStable(true))

	bluePool.Spec.Replicas = ptr.To(int32(0))
	s.applyAndWaitFor(t, ctx, c, isStable(true), bluePool)
	s.waitFor(t, ctx, c, rp, isStable(true))

	s.deleteAndWait(t, ctx, c, rp)
}

func (s *RedpandaControllerSuite) SetupSuite() {
	t := s.T()
	ctx := trace.Test(t)

	// Pre-pull the main Redpanda test image into k3d to avoid slow pulls
	// during test runs that can cause timeouts.
	importImages := []string{
		"localhost/redpanda-operator:dev",
		"ghcr.io/loft-sh/vcluster-pro:0.31.2",
		"registry.k8s.io/kube-controller-manager:v1.32.13",
		"registry.k8s.io/kube-apiserver:v1.32.13",
		"quay.io/jetstack/cert-manager-controller:v1.17.2",
		"quay.io/jetstack/cert-manager-cainjector:v1.17.2",
		"quay.io/jetstack/cert-manager-webhook:v1.17.2",
		"coredns/coredns:1.11.1",
		// Images used by TestLicense cases.
		"redpandadata/redpanda-unstable:v24.3.1-rc4",
		"redpandadata/redpanda-unstable:v24.3.1-rc8",
		"redpandadata/redpanda:v24.2.9",
	}
	if repo := os.Getenv("TEST_REDPANDA_REPO"); repo != "" {
		if version := os.Getenv("TEST_REDPANDA_VERSION"); version != "" {
			importImages = append(importImages, fmt.Sprintf("%s:%s", repo, version))
		}
	}

	s.env = testenv.New(t, testenv.Options{
		Scheme:             controller.V2Scheme,
		CRDs:               crds.All(),
		Logger:             log.FromContext(ctx),
		SkipVCluster:       true,
		WatchAllNamespaces: true,
		ImportImages:       importImages,
	})

	suiteClient := s.env.Client()

	s.env.SetupManager(s.setupRBAC(ctx, suiteClient), func(mgr multicluster.Manager) error {
		dialer := kube.NewPodDialer(mgr.GetLocalManager().GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr, nil).WithDialer(dialer.DialContext)

		require.NoError(t, (&redpanda.NodePoolReconciler{
			Manager: mgr,
		}).SetupWithManager(ctx, mgr, ""))

		// TODO should probably run other reconcilers here.
		return (&redpanda.RedpandaReconciler{
			Manager:       mgr,
			ClientFactory: s.clientFactory,
			LifecycleClient: lifecycle.NewResourceClient(mgr, lifecycle.V2ResourceManagers(
				lifecycle.Image{Repository: os.Getenv("TEST_REDPANDA_REPO"), Tag: os.Getenv("TEST_REDPANDA_VERSION")},
				lifecycle.Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
				lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false},
			)),
			UseNodePools: true,
		}).SetupWithManager(ctx, mgr, "")
	})

	// NB: t.Cleanup is used here to properly order our shutdown logic with
	// testenv which also uses t.Cleanup. Utilizing testify's TearDownAll makes
	// reasoning about the ordering more complicated than need be.
	t.Cleanup(func() {
		// Due to a fun race condition in testenv, we need to clear out all the
		// redpandas before we can let testenv shutdown. If we don't, the
		// operator's ClusterRoles and Roles may get GC'd before all the Redpandas
		// do which will prevent the operator from cleaning up said Redpandas.
		// List across all namespaces since tests create per-test namespaces.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()

		rawClient, err := client.New(s.env.RESTConfig(), client.Options{Scheme: controller.V2Scheme})
		require.NoError(t, err)

		var redpandas redpandav1alpha2.RedpandaList
		require.NoError(t, rawClient.List(cleanupCtx, &redpandas))

		for _, rp := range redpandas.Items {
			s.deleteAndWait(t, cleanupCtx, rawClient, &rp)
		}
	})
}

func (s *RedpandaControllerSuite) setupRBAC(ctx context.Context, c client.Client) string {
	t := s.T()

	roles, err := kube.DecodeYAML(operatorRBAC, c.Scheme())
	require.NoError(t, err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Merge the namespace-scoped Role rules into the ClusterRole so that the
	// ServiceAccount can operate in any test namespace (required for parallel
	// tests that each create their own namespace).
	clusterRole.Rules = append(clusterRole.Rules, role.Rules...)

	// Inject additional permissions required for running in testenv.
	clusterRole.Rules = append(clusterRole.Rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods/portforward"},
		Verbs:     []string{"*"},
	}, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods"},
		Verbs:     []string{"get", "list"},
	})

	name := "testenv-" + testenv.RandString(6)

	clusterRole.Name = name

	s.applyAndWait(t, ctx, c, clusterRole)
	s.applyAndWait(t, ctx, c,
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
		&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     clusterRole.Name,
			},
		},
	)

	return name
}

func (s *RedpandaControllerSuite) minimalRP() *redpandav1alpha2.Redpanda {
	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rp-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalRedpandaSpec(),
	}

	rp.Spec.ClusterSpec.Image.Repository = ptr.To(os.Getenv("TEST_REDPANDA_REPO"))

	return rp
}

func (s *RedpandaControllerSuite) minimalNodePool(cluster *redpandav1alpha2.Redpanda) *redpandav1alpha2.NodePool {
	np := &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pool-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalNodePoolSpec(cluster),
	}

	np.Spec.Image.Repository = ptr.To(os.Getenv("TEST_REDPANDA_REPO"))

	return np
}

func (s *RedpandaControllerSuite) deleteAndWait(t testing.TB, ctx context.Context, c client.Client, obj client.Object) {
	gvk, err := c.GroupVersionKindFor(obj)
	require.NoError(t, err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if err := c.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		// obj, might not exist at all. If so, no-op.
		if apierrors.IsNotFound(err) {
			return
		}
		require.NoError(t, err)
	}

	s.waitFor(t, ctx, c, obj, func(_ client.Object, err error) (bool, error) {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func (s *RedpandaControllerSuite) applyAndWait(t testing.TB, ctx context.Context, c client.Client, objs ...client.Object) {
	s.apply(t, ctx, c, objs...)
	s.waitUntilReady(t, ctx, c, objs...)
}

func (s *RedpandaControllerSuite) waitUntilReady(t testing.TB, ctx context.Context, c client.Client, objs ...client.Object) {
	for _, obj := range objs {
		s.waitFor(t, ctx, c, obj, func(obj client.Object, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			switch obj := obj.(type) {
			case *redpandav1alpha2.Redpanda:
				// Check "Stable" to make sure we're both done reconciling and we have a healthy cluster
				stable := apimeta.FindStatusCondition(obj.Status.Conditions, statuses.ClusterStable)
				if stable == nil {
					return false, nil
				}

				ready := stable.Status == metav1.ConditionTrue
				upToDate := obj.Generation == stable.ObservedGeneration
				return upToDate && ready, nil

			case *corev1.Secret, *corev1.ConfigMap, *corev1.ServiceAccount,
				*rbacv1.ClusterRole, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRoleBinding:
				return true, nil

			default:
				t.Fatalf("unhandled object %T in applyAndWait", obj)
				panic("unreachable")
			}
		})
	}
}

func (s *RedpandaControllerSuite) apply(t testing.TB, ctx context.Context, c client.Client, objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := c.GroupVersionKindFor(obj)
		require.NoError(t, err)

		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		obj.GetObjectKind().SetGroupVersionKind(gvk)

		require.NoError(t, c.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests"))) //nolint:staticcheck // TODO: migrate to client.Client.Apply()
	}
}

func (s *RedpandaControllerSuite) applyAndWaitFor(t testing.TB, ctx context.Context, c client.Client, cond func(client.Object, error) (bool, error), objs ...client.Object) {
	s.apply(t, ctx, c, objs...)
	for _, obj := range objs {
		s.waitFor(t, ctx, c, obj, cond)
	}
}

func (s *RedpandaControllerSuite) waitFor(t testing.TB, ctx context.Context, c client.Client, obj client.Object, cond func(client.Object, error) (bool, error)) {
	prefix := t.Name()
	start := time.Now()
	lastLog := time.Now()
	logEvery := 10 * time.Second

	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 10*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		err = c.Get(ctx, client.ObjectKeyFromObject(obj), obj)

		done, err = cond(obj, err)
		if done || err != nil {
			return done, err
		}

		if time.Since(lastLog) > logEvery {
			lastLog = time.Now()
			t.Logf("[%s] waited %s for %T %q", prefix, time.Since(start), obj, obj.GetName())
		}

		return false, nil
	})
	if err != nil {
		// Log diagnostic information on timeout to help debug flaky tests.
		if rp, ok := obj.(*redpandav1alpha2.Redpanda); ok {
			t.Logf("[%s] waitFor timed out after %s for Redpanda %q (generation=%d)", prefix, time.Since(start), rp.Name, rp.Generation)
			for _, cond := range rp.Status.Conditions {
				t.Logf("[%s]   condition %s: status=%s reason=%s observedGeneration=%d message=%s",
					prefix, cond.Type, cond.Status, cond.Reason, cond.ObservedGeneration, cond.Message)
			}
		}
		require.NoError(t, err)
	}
}

func TestPostInstallUpgradeJobIndex(t *testing.T) {
	dot, err := redpandachart.Chart.Dot(nil, helmette.Release{}, map[string]any{})
	require.NoError(t, err)

	state := &redpandachart.RenderState{Dot: dot, Values: helmette.Unwrap[redpandachart.Values](dot.Values), Chart: &dot.Chart, Files: &dot.Files, Release: &dot.Release}
	job := redpandachart.PostInstallUpgradeJob(state)

	// Assert that index 0 is the envsubst container as that's what
	// `clusterConfigfor` utilizes.
	require.Equal(t, "bootstrap-yaml-envsubst", job.Spec.Template.Spec.InitContainers[0].Name)
}

// TestControllerRBAC asserts that the declared Roles and ClusterRoles of the
// RedpandaReconciler line up with all the resource types it needs to manage.
func TestControllerRBAC(t *testing.T) {
	scheme := controller.V2Scheme

	expectedVerbs := []string{"create", "delete", "get", "list", "patch", "update", "watch"}

	roles, err := kube.DecodeYAML(operatorRBAC, scheme)
	require.NoError(t, err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	for _, typ := range redpandachart.Types() {
		gkvs, _, err := scheme.ObjectKinds(typ)
		require.NoError(t, err)

		require.Len(t, gkvs, 1)
		gvk := gkvs[0]

		group := gvk.Group
		kind := pluralize(gvk.Kind)

		rules := clusterRole.Rules
		idx := slices.IndexFunc(rules, func(rule rbacv1.PolicyRule) bool {
			return slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, kind)
		})
		if idx == -1 {
			rules = role.Rules
			idx = slices.IndexFunc(rules, func(rule rbacv1.PolicyRule) bool {
				return slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, kind)
			})
		}

		require.NotEqual(t, -1, idx, "missing rules for %s %s", gvk.Group, kind)
		require.EqualValues(t, expectedVerbs, rules[idx].Verbs, "incorrect verbs for %s %s", gvk.Group, kind)
	}
}

func pluralize(kind string) string {
	switch kind[len(kind)-1] {
	case 's':
		return strings.ToLower(kind) + "es"

	default:
		return strings.ToLower(kind) + "s"
	}
}

func convertToRawExtension(t testing.TB, cfg any) *runtime.RawExtension {
	asJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	return &runtime.RawExtension{Raw: asJSON}
}
