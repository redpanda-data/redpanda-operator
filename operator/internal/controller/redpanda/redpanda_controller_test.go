// Copyright 2025 Redpanda Data, Inc.
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

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
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
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

	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	clientFactory internalclient.ClientFactory
}

var (
	_ suite.SetupAllSuite  = (*RedpandaControllerSuite)(nil)
	_ suite.SetupTestSuite = (*RedpandaControllerSuite)(nil)
)

func (s *RedpandaControllerSuite) TestMergerOfRedpandas() {
	disableRPCTLS := &redpandav1alpha2.Listeners{
		RPC: &redpandav1alpha2.RPC{
			TLS: &redpandav1alpha2.ListenerTLS{
				Enabled: ptr.To(false),
			},
		},
	}

	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Listeners = disableRPCTLS

	s.applyAndWait(rp)

	rpSecond := s.minimalRP()
	rpSecond.Spec.ClusterSpec.Listeners = disableRPCTLS

	// Override seed_servers with the first cluster DNS
	var cm corev1.ConfigMap
	s.Require().NoError(s.client.Get(s.ctx, client.ObjectKey{Name: rp.Name, Namespace: rp.Namespace}, &cm))

	var rpConfig map[string]map[string]any
	yaml.Unmarshal([]byte(cm.Data[clusterconfiguration.RedpandaYamlTemplateFile]), &rpConfig)

	rpSecond.Spec.ClusterSpec.Config.Node = s.ConvertToRawExtension(map[string]any{
		"seed_servers": rpConfig["redpanda"]["seed_servers"],
	})

	s.applyAndWait(rpSecond)

	adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, rp)
	s.Require().NoError(err)
	defer adminClient.Close()

	br, err := adminClient.Brokers(s.ctx)
	s.Require().NoError(err)

	s.Require().Len(br, 2)
	s.Require().Contains(br[0].InternalRPCAddress, rp.Name)
	s.Require().Contains(br[1].InternalRPCAddress, rpSecond.Name)

	s.deleteAndWait(rp)
	s.deleteAndWait(rpSecond)
}

func (s *RedpandaControllerSuite) TestManaged() {
	rp := s.minimalRP()

	s.applyAndWait(rp)

	// We've had default feature annotations applied.
	s.Equal("true", rp.Annotations[feature.V2Managed.Key])

	rp.Annotations[feature.V2Managed.Key] = "false"
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(10) // Better hope this feature works.

	// The signifier that this a cluster is not being "managed" any more is
	// that its finalizes have been removed.
	s.applyAndWaitFor(func(o client.Object, err error) (bool, error) {
		return len(o.(*redpandav1alpha2.Redpanda).Finalizers) == 0, nil
	})

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestObjectsGCed() {
	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Console.Enabled = ptr.To(true)

	s.applyAndWait(rp)

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
		s.Require().NoError(s.client.Create(s.ctx, secret))
	}

	// Assert that the console deployment exists
	s.EventuallyWithT(func(t *assert.CollectT) {
		var deployments appsv1.DeploymentList
		assert.NoError(t, s.client.List(s.ctx, &deployments, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name, "app.kubernetes.io/name": "console"}))
		assert.Len(t, deployments.Items, 1)
	}, time.Minute, time.Second, "console deployment not scheduled")

	rp.Spec.ClusterSpec.Console.Enabled = ptr.To(false)
	s.applyAndWait(rp)

	// Assert that the console deployment has been garbage collected.
	s.EventuallyWithT(func(t *assert.CollectT) {
		var deployments appsv1.DeploymentList
		assert.NoError(t, s.client.List(s.ctx, &deployments, client.MatchingLabels{"app.kubernetes.io/instance": rp.Name, "app.kubernetes.io/name": "console"}))
		assert.Len(t, deployments.Items, 0)
	}, time.Minute, time.Second, "console deployment not GC'd")

	// Assert that our previously created secrets have not been GC'd.
	for _, secret := range secrets {
		key := client.ObjectKeyFromObject(secret)
		s.Require().NoError(s.client.Get(s.ctx, key, secret))
	}

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestTPLValues() {
	s.T().Skip("invalid / broken due to changes in chart v25.1.1 (podTemplate)")

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
	s.applyAndWait(rp)

	var sts appsv1.StatefulSet
	s.NoError(s.client.Get(s.ctx, types.NamespacedName{Name: rp.Name, Namespace: rp.Namespace}, &sts))

	s.Contains(sts.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: "test-extra-volume",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{DefaultMode: ptr.To(int32(508)), SecretName: fmt.Sprintf("%s-sts-lifecycle", rp.Name)},
		},
	})
	for _, c := range sts.Spec.Template.Spec.InitContainers {
		if c.Name == "bootstrap-yaml-envsubst" {
			continue
		}

		s.Contains(c.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})

		if c.Name == "test-init-container" {
			s.Equal(c.Command, []string{"/bin/sh", "-c"})
			s.Equal(c.Args, []string{"set -xe\necho \"Hello World!\""})
			s.Equal(c.Image, "alpine:latest")
		}
	}
	for _, c := range sts.Spec.Template.Spec.Containers {
		s.Contains(c.VolumeMounts, corev1.VolumeMount{Name: "test-extra-volume", MountPath: "/FAKE/LIFECYCLE"})
	}

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestExternalSecretInjection() {
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

	s.T().Log("Applying secret injected-value")
	s.applyAndWait(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "injected-value",
		},
		Data: map[string]string{
			"value-1": "1003",
		},
	})
	s.applyAndWait(rp)

	adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, rp)
	require.NoError(s.T(), err)
	config, err := adminClient.Config(s.ctx, false)
	require.NoError(s.T(), err)

	require.Equal(s.T(), float64(1003), config["segment_appender_flush_timeout_ms"])
}

func (s *RedpandaControllerSuite) TestClusterSettings() {
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
	s.applyAndWait(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creds",
		},
		Data: map[string][]byte{
			"access_key": []byte("VURYSECRET"),
		},
	})
	s.applyAndWait(rp)

	setConfig := func(cfg map[string]any) func() {
		asJSON, err := json.Marshal(cfg)
		s.Require().NoError(err)

		rp.Spec.ClusterSpec.Config.Cluster = &runtime.RawExtension{Raw: asJSON}
		s.apply(rp)
		return func() {
			s.waitUntilReady(rp)
			s.waitFor(rp, func(o client.Object, err error) (bool, error) {
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

	for _, c := range cases {
		s.Run(c.Name, func() {
			s.waitUntilReady(rp)

			adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, rp)
			s.Require().NoError(err)
			defer adminClient.Close()

			var initialVersion int64
			s.EventuallyWithT(func(t *assert.CollectT) {
				st, err := adminClient.ClusterConfigStatus(s.ctx, false)
				if !assert.NoError(t, err) {
					s.T().Logf("[%s] getting cluster config status failed with: %v", time.Now().Format(time.DateTime), err)
					return
				}
				initialVersion = slices.MaxFunc(st, func(a, b rpadmin.ConfigStatus) int {
					return int(a.ConfigVersion - b.ConfigVersion)
				}).ConfigVersion
			}, 5*time.Minute, time.Second)

			waitFn := setConfig(c.In)
			s.EventuallyWithT(func(t *assert.CollectT) {
				st, err := adminClient.ClusterConfigStatus(s.ctx, false)
				if !assert.NoError(t, err) {
					s.T().Logf("[%s] getting cluster config status failed with: %v", time.Now().Format(time.DateTime), err)
					return
				}
				s.T().Logf("[%s] cluster status: %v", time.Now().Format(time.DateTime), st)
				currVersion := slices.MinFunc(st, func(a, b rpadmin.ConfigStatus) int {
					return int(a.ConfigVersion - b.ConfigVersion)
				}).ConfigVersion

				// Only operator should change cluster configuration once. If there is any other party that changes
				// Redpanda cluster configuration, it is unexpected and should be investigated.
				assert.Equal(t, initialVersion+1, currVersion, "current config version should increase only by one")

				assert.False(t, slices.ContainsFunc(st, func(cs rpadmin.ConfigStatus) bool {
					return cs.Restart
				}), "expected no brokers to need restart")
			}, 5*time.Minute, time.Second)
			// wait for the cluster to be ready and the configuration synced
			waitFn()

			config, err := adminClient.Config(s.ctx, false)
			s.Require().NoError(err)

			arr := config["superusers"].([]any)

			sort.Slice(arr, func(i, j int) bool {
				return arr[i].(string) < arr[j].(string)
			})

			// Only assert that c.Expected is a subset of the set config.
			// The chart/operator injects a bunch of "useful" values by default.
			s.Subset(config, c.Expected)
		})
	}

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestClusterSettingsRegressionSuperusers() {
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

	s.applyAndWait(rp)

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

	s.applyAndWait(rp)

	s.Require().Equal(initialVersion, rp.Status.ConfigVersion)

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestLicenseReal() {
	const (
		LicenseEnvVar             = "REDPANDA_SAMPLE_LICENSE"
		RedpandaLicenseSecretName = "rp-license"
		RedpandaLicenseKeyName    = "license"
	)
	license := os.Getenv(LicenseEnvVar)
	if license == "" {
		s.T().Skipf("License environment variable %q is not provided", LicenseEnvVar)
	}

	s.apply(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: RedpandaLicenseSecretName,
		},
		Data: map[string][]byte{
			RedpandaLicenseKeyName: []byte(license),
		},
	})

	redpandas := map[string]*redpandav1alpha2.Redpanda{}

	rp := s.minimalRP()
	rp.Spec.ClusterSpec.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{
		Spec: &redpandav1alpha2.PodSpecApplyConfiguration{
			PodSpecApplyConfiguration: &applycorev1.PodSpecApplyConfiguration{
				Containers: []applycorev1.ContainerApplyConfiguration{{
					Name: ptr.To("redpanda"),
					Env: []applycorev1.EnvVarApplyConfiguration{
						*applycorev1.EnvVar().WithName("__REDPANDA_DISABLE_BUILTIN_TRIAL_LICENSE").WithValue("true"),
					},
				}},
			},
		},
	}
	rp.Spec.ClusterSpec.Enterprise = &redpandav1alpha2.Enterprise{
		License: &license,
	}

	rp.Spec.ClusterSpec.Config.Cluster = s.ConvertToRawExtension(map[string]bool{
		"core_balancing_continuous": true,
	})

	redpandas["literal-license"] = rp

	rp = rp.DeepCopy()
	rp.Spec.ClusterSpec.Enterprise = &redpandav1alpha2.Enterprise{
		License: nil,
		LicenseSecretRef: &redpandav1alpha2.EnterpriseLicenseSecretRef{
			Key:  ptr.To(RedpandaLicenseKeyName),
			Name: ptr.To(RedpandaLicenseSecretName),
		},
	}

	redpandas["license-in-secret"] = rp

	for testCaseName, tc := range redpandas {
		s.T().Run(testCaseName, func(t *testing.T) {
			var licenseStatus *redpandav1alpha2.RedpandaLicenseStatus
			s.applyAndWaitFor(func(o client.Object, err error) (bool, error) {
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

			s.Require().Equal(&redpandav1alpha2.RedpandaLicenseStatus{
				Violation:     false,
				InUseFeatures: []string{"core_balancing_continuous"},
				Expired:       ptr.To(false),
				Type:          ptr.To("enterprise"),
				Organization:  ptr.To("redpanda-testing"),
				Expiration:    licenseStatus.Expiration,
			}, licenseStatus)

			s.deleteAndWait(tc)
		})
	}
}

func (s *RedpandaControllerSuite) TestLicense() {
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

	for _, c := range cases {
		rp := s.minimalRP()
		rp.Spec.ClusterSpec.Image = &redpandav1alpha2.RedpandaImage{
			Repository: ptr.To(c.image.repository),
			Tag:        ptr.To(c.image.tag),
		}
		if !c.license {
			rp.Spec.ClusterSpec.Statefulset.PodTemplate = &redpandav1alpha2.PodTemplate{
				Spec: &redpandav1alpha2.PodSpecApplyConfiguration{
					PodSpecApplyConfiguration: &applycorev1.PodSpecApplyConfiguration{
						Containers: []applycorev1.ContainerApplyConfiguration{{
							Name: ptr.To("redpanda"),
							Env: []applycorev1.EnvVarApplyConfiguration{
								*applycorev1.EnvVar().WithName("__REDPANDA_DISABLE_BUILTIN_TRIAL_LICENSE").WithValue("true"),
							},
						}},
					},
				},
			}
		}

		var condition metav1.Condition
		var licenseStatus *redpandav1alpha2.RedpandaLicenseStatus
		s.applyAndWaitFor(func(o client.Object, err error) (bool, error) {
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

		name := fmt.Sprintf("%s/%s (license: %t)", c.image.repository, c.image.tag, c.license)
		message := fmt.Sprintf("%s - %s != %s", name, c.expected, condition.Message)
		s.Require().Equal(c.expected, condition.Message, message)

		if c.expectedLicenseStatus == nil && licenseStatus != nil {
			s.T().Fatalf("%s does not have a nil license %s", name, licenseStatus.String())
		}

		if c.expectedLicenseStatus != nil {
			s.Require().NotNil(licenseStatus, "%s does has a nil license", name)
			s.Require().Equal(licenseStatus.Expired, c.expectedLicenseStatus.Expired, "%s license expired field does not match", name)
			s.Require().EqualValues(licenseStatus.InUseFeatures, c.expectedLicenseStatus.InUseFeatures, "%s license valid features do not match", name)
			s.Require().Equal(licenseStatus.Organization, c.expectedLicenseStatus.Organization, "%s license organization field does not match", name)
			s.Require().Equal(licenseStatus.Type, c.expectedLicenseStatus.Type, "%s license type field does not match", name)
			s.Require().Equal(licenseStatus.Violation, c.expectedLicenseStatus.Violation, "%s license violation field does not match", name)

			// only do the expiration check if the license isn't already expired
			if licenseStatus.Expired != nil && !*licenseStatus.Expired {
				expectedExpiration := c.expectedLicenseStatus.Expiration.UTC()
				actualExpiration := licenseStatus.Expiration.UTC()

				rangeFactor := 5 * time.Minute
				// add some fudge factor so that we don't fail with flakiness due to tests being run at
				// the change of a couple of minutes that causes the date to be rolled over by some factor
				if !(expectedExpiration.Add(rangeFactor).After(actualExpiration) &&
					expectedExpiration.Add(-rangeFactor).Before(actualExpiration)) {
					s.T().Fatalf("%s does not match expected expiration: %s != %s", name, actualExpiration, expectedExpiration)
				}
			}
		}

		s.deleteAndWait(rp)
	}
}

func (s *RedpandaControllerSuite) TestScaling() {
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
	s.applyAndWaitFor(isStable, rp)

	// Scale down to 3.
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)
	s.applyAndWaitFor(isStable, rp)

	// And then back up to 5.
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(5)
	s.applyAndWaitFor(isStable, rp)

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestNodePools() {
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
	s.applyAndWaitFor(isStable(true), rp)

	// add another broker via a nodepool.
	pool := s.minimalNodePool(rp)
	s.applyAndWaitFor(isStable(true), pool)

	s.deleteAndWait(rp)

	// we should go unstable due to an unhealthy broker
	s.waitFor(pool, isStable(false))
	// and eventually we should fully unbind
	s.waitFor(pool, func(o client.Object, err error) (bool, error) {
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
	s.deleteAndWait(pool)
}

func (s *RedpandaControllerSuite) TestNodePoolsBlueGreen() {
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
	s.apply(rp)

	// add a nodepool with 3 brokers.
	bluePool := s.minimalNodePool(rp)
	bluePool.Spec.Replicas = ptr.To(int32(3))
	s.applyAndWaitFor(isStable(true), bluePool)
	// now we make sure the cluster is stable each modification
	s.waitFor(rp, isStable(true))

	greenPool := s.minimalNodePool(rp)
	greenPool.Spec.Replicas = ptr.To(int32(0))
	s.applyAndWaitFor(isStable(true), greenPool)
	s.waitFor(rp, isStable(true))

	greenPool.Spec.Replicas = ptr.To(int32(3))
	s.applyAndWaitFor(isStable(true), greenPool)
	s.waitFor(rp, isStable(true))

	bluePool.Spec.Replicas = ptr.To(int32(0))
	s.applyAndWaitFor(isStable(true), bluePool)
	s.waitFor(rp, isStable(true))

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) SetupTest() {
	prev := s.ctx
	s.ctx = trace.Test(s.T())
	s.T().Cleanup(func() {
		s.ctx = prev
	})
}

func (s *RedpandaControllerSuite) SetupSuite() {
	t := s.T()
	s.ctx = trace.Test(t)

	s.env = testenv.New(t, testenv.Options{
		Scheme:       controller.V2Scheme,
		CRDs:         crds.All(),
		Logger:       log.FromContext(s.ctx),
		SkipVCluster: true,
		ImportImages: []string{
			"localhost/redpanda-operator:dev",
		},
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		s.Require().NoError((&redpanda.NodePoolReconciler{
			Client: mgr.GetClient(),
		}).SetupWithManager(s.ctx, mgr))

		// TODO should probably run other reconcilers here.
		return (&redpanda.RedpandaReconciler{
			Client:        mgr.GetClient(),
			KubeConfig:    mgr.GetConfig(),
			EventRecorder: mgr.GetEventRecorderFor("Redpanda"),
			ClientFactory: s.clientFactory,
			LifecycleClient: lifecycle.NewResourceClient(mgr, lifecycle.V2ResourceManagers(
				lifecycle.Image{Repository: "redpandadata/redpanda", Tag: os.Getenv("TEST_REDPANDA_VERSION")},
				lifecycle.Image{Repository: "localhost/redpanda-operator", Tag: "dev"},
				lifecycle.CloudSecretsFlags{CloudSecretsEnabled: false},
			)),
			UseNodePools: true,
		}).SetupWithManager(s.ctx, mgr)
	})

	// NB: t.Cleanup is used here to properly order our shutdown logic with
	// testenv which also uses t.Cleanup. Utilizing testify's TearDownAll makes
	// reasoning about the ordering more complicated than need be.
	t.Cleanup(func() {
		// Due to a fun race condition in testenv, we need to clear out all the
		// redpandas before we can let testenv shutdown. If we don't, the
		// operator's ClusterRoles and Roles may get GC'd before all the Redpandas
		// do which will prevent the operator from cleaning up said Redpandas.
		var redpandas redpandav1alpha2.RedpandaList
		s.NoError(s.env.Client().List(s.ctx, &redpandas))

		for _, rp := range redpandas.Items {
			s.deleteAndWait(&rp)
		}
	})
}

func (s *RedpandaControllerSuite) setupRBAC() string {
	roles, err := kube.DecodeYAML(operatorRBAC, s.client.Scheme())
	s.Require().NoError(err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Inject additional permissions required for running in testenv.
	// For this style of tests we port-forward into Pods to emulate "in-cluster networking"
	// and we need list and get pods in kube-system to emulate in cluster DNS.
	// As our client is namespace scoped, it's non-trivial to make a kube-system
	// dedicated role so we're settling with overscoped Pod get and list
	// permissions.
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

	role.Name = name
	role.Namespace = s.env.Namespace()
	clusterRole.Name = name
	clusterRole.Namespace = s.env.Namespace()

	s.applyAndWait(roles...)
	s.applyAndWait(
		&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
		&rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: "ServiceAccount", Namespace: s.env.Namespace(), Name: name},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "Role",
				Name:     role.Name,
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
	return &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "rp-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalRedpandaSpec(),
	}
}

func (s *RedpandaControllerSuite) minimalNodePool(cluster *redpandav1alpha2.Redpanda) *redpandav1alpha2.NodePool {
	return &redpandav1alpha2.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pool-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
			Annotations: make(map[string]string),
		},
		Spec: redpandav1alpha2.MinimalNodePoolSpec(cluster),
	}
}

func (s *RedpandaControllerSuite) deleteAndWait(obj client.Object) {
	gvk, err := s.client.GroupVersionKindFor(obj)
	s.NoError(err)

	obj.SetManagedFields(nil)
	obj.GetObjectKind().SetGroupVersionKind(gvk)

	if err := s.client.Delete(s.ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		// obj, might not exist at all. If so, no-op.
		if apierrors.IsNotFound(err) {
			return
		}
		s.Require().NoError(err)
	}

	s.waitFor(obj, func(_ client.Object, err error) (bool, error) {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func (s *RedpandaControllerSuite) applyAndWait(objs ...client.Object) {
	s.apply(objs...)
	s.waitUntilReady(objs...)
}

func (s *RedpandaControllerSuite) waitUntilReady(objs ...client.Object) {
	for _, obj := range objs {
		s.waitFor(obj, func(obj client.Object, err error) (bool, error) {
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
				s.T().Fatalf("unhandled object %T in applyAndWait", obj)
				panic("unreachable")
			}
		})
	}
}

func (s *RedpandaControllerSuite) apply(objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := s.client.GroupVersionKindFor(obj)
		s.NoError(err)

		obj.SetManagedFields(nil)
		obj.SetResourceVersion("")
		obj.GetObjectKind().SetGroupVersionKind(gvk)

		s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))
	}
}

func (s *RedpandaControllerSuite) applyAndWaitFor(cond func(client.Object, error) (bool, error), objs ...client.Object) {
	s.apply(objs...)
	for _, obj := range objs {
		s.waitFor(obj, cond)
	}
}

func (s *RedpandaControllerSuite) waitFor(obj client.Object, cond func(client.Object, error) (bool, error)) {
	start := time.Now()
	lastLog := time.Now()
	logEvery := 10 * time.Second

	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		err = s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)

		done, err = cond(obj, err)
		if done || err != nil {
			return done, err
		}

		if time.Since(lastLog) > logEvery {
			lastLog = time.Now()
			s.T().Logf("waited %s for %T %q", time.Since(start), obj, obj.GetName())
		}

		return false, nil
	}))
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

func (s *RedpandaControllerSuite) ConvertToRawExtension(cfg any) *runtime.RawExtension {
	asJSON, err := json.Marshal(cfg)
	s.Require().NoError(err)

	return &runtime.RawExtension{Raw: asJSON}
}
