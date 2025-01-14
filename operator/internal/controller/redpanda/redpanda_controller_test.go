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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fluxcd/helm-controller/api/v2beta2"
	fluxclient "github.com/fluxcd/pkg/runtime/client"
	sourcecontrollerv1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/flux"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testenv"
	internalclient "github.com/redpanda-data/redpanda-operator/operator/pkg/client"
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// operatorRBAC is the ClusterRole and Role generated via controller-gen and
// goembeded so it can be used for tests.
//
//go:embed role.yaml
var operatorRBAC []byte

// NB: This test setup is largely incompatible with webhooks. Though we might
// be able to figure something freaky out.
func TestRedpandaController(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long running test as -short was specified")
	}
	suite.Run(t, new(RedpandaControllerSuite))
}

type RedpandaControllerSuite struct {
	suite.Suite

	ctx           context.Context
	env           *testenv.Env
	client        client.Client
	clientFactory internalclient.ClientFactory
}

var _ suite.SetupAllSuite = (*RedpandaControllerSuite)(nil)

func (s *RedpandaControllerSuite) TestObjectsGCed() {
	rp := s.minimalRP(false)
	rp.Spec.ClusterSpec.Console.Enabled = ptr.To(true)
	rp.Spec.ClusterSpec.Connectors = &redpandav1alpha2.RedpandaConnectors{
		Enabled: ptr.To(true),
	}

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
	rp := s.minimalRP(false)

	extraVolumeMount := ptr.To(`- name: test-extra-volume
  mountPath: {{ upper "/fake/lifecycle" }}`)

	rp.Spec.ClusterSpec.Statefulset.ExtraVolumeMounts = extraVolumeMount
	rp.Spec.ClusterSpec.Statefulset.ExtraVolumes = ptr.To(fmt.Sprintf(`- name: test-extra-volume
  secret:
    secretName: %s-sts-lifecycle
    defaultMode: 0774`, rp.Name))
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

func (s *RedpandaControllerSuite) TestManagedDecommission() {
	rp := s.minimalRP(false)
	rp.Spec.ClusterSpec.Statefulset.Replicas = ptr.To(3)

	s.applyAndWait(rp)

	adminAPI, err := s.clientFactory.RedpandaAdminClient(s.ctx, rp)
	s.Require().NoError(err)
	defer adminAPI.Close()

	beforeBrokers, err := adminAPI.Brokers(s.ctx)
	s.Require().NoError(err)

	rp.Annotations = map[string]string{
		"operator.redpanda.com/managed-decommission": "2999-12-31T00:00:00Z",
	}

	s.applyAndWaitFor(func(o client.Object) bool {
		rp := o.(*redpandav1alpha2.Redpanda)

		for _, cond := range rp.Status.Conditions {
			if cond.Type == "ManagedDecommission" {
				return cond.Status == metav1.ConditionTrue
			}
		}
		return false
	}, rp)

	s.waitFor(rp, func(o client.Object) bool {
		rp := o.(*redpandav1alpha2.Redpanda)

		for _, cond := range rp.Status.Conditions {
			if cond.Type == "ManagedDecommission" {
				return false
			}
		}

		_, ok := rp.Annotations["operator.redpanda.com/managed-decommission"]

		return !ok // Managed decommission is finished when the annotation is removed.
	})

	afterBrokers, err := adminAPI.Brokers(s.ctx)
	s.Require().NoError(err)

	// After performing a managed decommission we should observe a complete
	// replacement of brokers.
	beforeIDs := map[int]struct{}{}
	for _, broker := range beforeBrokers {
		beforeIDs[broker.NodeID] = struct{}{}
	}

	afterIDs := map[int]struct{}{}
	for _, broker := range afterBrokers {
		afterIDs[broker.NodeID] = struct{}{}
	}

	for id := range beforeIDs {
		s.NotContainsf(afterIDs, id, "broker ID %d unexpectedly present after managed decommission", id)
	}

	for id := range afterIDs {
		s.NotContainsf(beforeIDs, id, "broker ID %d unexpectedly present after managed decommission", id)
	}

	s.Len(beforeIDs, 3)
	s.Len(afterIDs, 3)

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestClusterSettings() {
	rp := s.minimalRP(false)
	s.applyAndWait(rp)

	setConfig := func(cfg map[string]any) {
		asJSON, err := json.Marshal(cfg)
		s.Require().NoError(err)

		rp.Spec.ClusterSpec.Config.Cluster = &runtime.RawExtension{Raw: asJSON}
		s.applyAndWait(rp)
		s.applyAndWaitFor(func(o client.Object) bool {
			rp := o.(*redpandav1alpha2.Redpanda)
			for _, cond := range rp.Status.Conditions {
				if cond.Type == redpandav1alpha2.ClusterConfigSynced {
					return cond.ObservedGeneration == rp.Generation && cond.Status == metav1.ConditionTrue
				}
			}
			return false
		}, rp)
	}
	s.applyAndWait(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creds",
		},
		Data: map[string][]byte{
			"access_key": []byte("VURYSECRET"),
		},
	})

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

	cases := []struct {
		In       map[string]any
		Expected map[string]any
	}{
		{
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
			In: map[string]any{
				"enable_transactions":         true,
				"enable_schema_id_validation": "none",
				// TODO: Minor bug in the helm chart here, setting superusers
				// in cluster.config results in the bootstrap users getting
				// excluded.
				// "superusers":                  []any{"jimbob"},
			},
			Expected: map[string]any{
				"admin_api_require_auth":    true,
				"cloud_storage_access_key":  "VURYSECRET",
				"cloud_storage_disable_tls": true,
				"superusers":                []any{"alice", "bob", "kubernetes-controller"},
				// "superusers":                []any{"alice", "bob", "jimbob", "kubernetes-controller"},
			},
		},
	}

	for _, c := range cases {
		setConfig(c.In)

		adminClient, err := s.clientFactory.RedpandaAdminClient(s.ctx, rp)
		s.Require().NoError(err)
		defer adminClient.Close()

		config, err := adminClient.Config(s.ctx, false)
		s.Require().NoError(err)

		// Only assert that c.Expected is a subset of the set config.
		// The chart/operator injects a bunch of "useful" values by default.
		s.Subset(config, c.Expected)
	}

	s.deleteAndWait(rp)
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
		expected: "Expired",
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
		expected: "Valid",
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
		expected: "Not Present",
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
		expected: "Not Present",
		expectedLicenseStatus: &redpandav1alpha2.RedpandaLicenseStatus{
			Violation:     false,
			InUseFeatures: []string{},
		},
	}}

	for _, c := range cases {
		rp := s.minimalRP(false)
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
		s.applyAndWaitFor(func(o client.Object) bool {
			rp := o.(*redpandav1alpha2.Redpanda)

			for _, cond := range rp.Status.Conditions {
				if cond.Type == redpandav1alpha2.ClusterLicenseValid {
					// grab the first non-unknown status
					if cond.Status != metav1.ConditionUnknown {
						condition = cond
						licenseStatus = rp.Status.LicenseStatus
						return true
					}
					return false
				}
			}
			return false
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

func (s *RedpandaControllerSuite) TestPodTemplateOverrides() {
	rp := s.minimalRP(true)

	rp.Spec.ClusterSpec.PostInstallJob = &redpandav1alpha2.PostInstallJob{
		PodTemplate: &redpandav1alpha2.PodTemplate{
			Spec: &redpandav1alpha2.PodSpecApplyConfiguration{
				PodSpecApplyConfiguration: &applycorev1.PodSpecApplyConfiguration{
					InitContainers: []applycorev1.ContainerApplyConfiguration{
						{
							Name: ptr.To("bootstrap-yaml-envsubst"),
							Resources: &applycorev1.ResourceRequirementsApplyConfiguration{
								Requests: &corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("10Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	s.applyAndWait(rp)

	var jobs batchv1.JobList
	s.NoError(s.client.List(s.ctx, &jobs, client.MatchingLabels{
		"app.kubernetes.io/instance": rp.Name,
	}))

	s.Len(jobs.Items, 1, "expected to find 1 post install/upgrade job")
	for _, job := range jobs.Items {
		s.True(job.Spec.Template.Spec.InitContainers[0].Resources.Requests[corev1.ResourceMemory].Equal(resource.MustParse("10Mi")))
	}

	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestConnectorsIntegration() {
	rp := s.minimalRP(false)

	rp.Spec.ClusterSpec.Connectors = &redpandav1alpha2.RedpandaConnectors{
		Enabled: ptr.To(true),
	}

	s.applyAndWait(rp)
	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) TestUpgradeRollback() {
	rp := s.minimalRP(true)
	rp.Spec.ChartRef.Upgrade = &redpandav1alpha2.HelmUpgrade{
		Remediation: &v2beta2.UpgradeRemediation{
			Retries:  1,
			Strategy: ptr.To(v2beta2.RollbackRemediationStrategy),
		},
	}
	rp.Spec.ClusterSpec.Image.Tag = ptr.To("v23.2.3")

	s.applyAndWait(rp)

	// Apply a broken upgrade and make sure things rollback

	rp.Spec.ChartRef.Timeout = ptr.To(metav1.Duration{Duration: 30 * time.Second})
	rp.Spec.ClusterSpec.Image.Tag = ptr.To("v23.99.99")

	s.applyAndWaitFor(func(o client.Object) bool {
		rp := o.(*redpandav1alpha2.Redpanda)

		for _, cond := range rp.Status.Conditions {
			if cond.Type == redpandav1alpha2.ReadyCondition {
				if cond.Status == metav1.ConditionFalse && cond.Reason == "ArtifactFailed" {
					return true
				}
				return false
			}
		}
		return false
	}, rp)

	s.waitFor(&v2beta2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.GetName(),
			Namespace: rp.GetNamespace(),
		},
	}, func(o client.Object) bool {
		helmRelease := o.(*v2beta2.HelmRelease)

		return helmRelease.Status.UpgradeFailures >= 1 && slices.ContainsFunc(helmRelease.Status.Conditions, func(cond metav1.Condition) bool {
			return cond.Type == redpandav1alpha2.ReadyCondition && cond.Status == metav1.ConditionFalse && cond.Reason == "UpgradeFailed"
		})
	})

	s.waitFor(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.GetName(),
			Namespace: rp.GetNamespace(),
		},
	}, func(o client.Object) bool {
		statefulSet := o.(*appsv1.StatefulSet)

		// check that we have a ready replica still because we've rolled back
		return statefulSet.Status.ReadyReplicas == 1
	})

	// Apply the good upgrade

	rp.Spec.ChartRef.Timeout = ptr.To(metav1.Duration{Duration: 3 * time.Minute})
	rp.Spec.ClusterSpec.Image.Tag = ptr.To("v23.2.10")

	s.applyAndWait(rp)

	s.waitFor(&v2beta2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.GetName(),
			Namespace: rp.GetNamespace(),
		},
	}, func(o client.Object) bool {
		helmRelease := o.(*v2beta2.HelmRelease)

		return slices.ContainsFunc(helmRelease.Status.Conditions, func(cond metav1.Condition) bool {
			return cond.Type == redpandav1alpha2.ReadyCondition && cond.Status == metav1.ConditionTrue && cond.Reason == "UpgradeSucceeded"
		})
	})

	s.waitFor(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rp.GetName(),
			Namespace: rp.GetNamespace(),
		},
	}, func(o client.Object) bool {
		statefulSet := o.(*appsv1.StatefulSet)

		return statefulSet.Status.ReadyReplicas == 1
	})
	s.deleteAndWait(rp)
}

func (s *RedpandaControllerSuite) SetupSuite() {
	t := s.T()

	// TODO SetupManager currently runs with admin permissions on the cluster.
	// This will allow the operator's ClusterRole and Role to get out of date.
	// Ideally, we should bind the declared permissions of the operator to the
	// rest config given to the manager.
	s.ctx = context.Background()
	s.env = testenv.New(t, testenv.Options{
		Scheme: controller.V2Scheme,
		CRDs:   crds.All(),
		Logger: testr.New(t),
	})

	s.client = s.env.Client()

	s.env.SetupManager(s.setupRBAC(), func(mgr ctrl.Manager) error {
		controllers := flux.NewFluxControllers(mgr, fluxclient.Options{}, fluxclient.KubeConfigOptions{})
		for _, controller := range controllers {
			if err := controller.SetupWithManager(s.ctx, mgr); err != nil {
				return err
			}
		}

		dialer := kube.NewPodDialer(mgr.GetConfig())
		s.clientFactory = internalclient.NewFactory(mgr.GetConfig(), mgr.GetClient()).WithDialer(dialer.DialContext)

		// TODO should probably run other reconcilers here.
		if err := (&redpanda.RedpandaReconciler{
			Client:            mgr.GetClient(),
			Scheme:            mgr.GetScheme(),
			EventRecorder:     mgr.GetEventRecorderFor("Redpanda"),
			ClientFactory:     s.clientFactory,
			HelmRepositoryURL: "https://charts.redpanda.com/",
		}).SetupWithManager(s.ctx, mgr); err != nil {
			return err
		}

		return (&redpanda.ManagedDecommissionReconciler{
			Client:        mgr.GetClient(),
			EventRecorder: mgr.GetEventRecorderFor("Redpanda"),
			ClientFactory: s.clientFactory,
		}).SetupWithManager(mgr)
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

		// For some reason, it seems that HelmCharts can get abandoned. Clean
		// them up to prevent hanging the NS deletion.
		var helmCharts sourcecontrollerv1beta2.HelmChartList
		s.NoError(s.env.Client().List(s.ctx, &helmCharts))

		for _, chart := range helmCharts.Items {
			s.deleteAndWait(&chart)
		}
	})
}

func (s *RedpandaControllerSuite) setupRBAC() string {
	roles, err := kube.DecodeYAML(operatorRBAC, s.client.Scheme())
	s.Require().NoError(err)

	role := roles[1].(*rbacv1.Role)
	clusterRole := roles[0].(*rbacv1.ClusterRole)

	// Inject additional permissions required for running in testenv.
	role.Rules = append(role.Rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"pods/portforward"},
		Verbs:     []string{"*"},
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

func (s *RedpandaControllerSuite) minimalRP(useFlux bool) *redpandav1alpha2.Redpanda {
	return &redpandav1alpha2.Redpanda{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rp-" + testenv.RandString(6), // GenerateName doesn't play nice with SSA.
		},
		Spec: redpandav1alpha2.RedpandaSpec{
			ChartRef: redpandav1alpha2.ChartRef{
				UseFlux: ptr.To(useFlux),
				Upgrade: &redpandav1alpha2.HelmUpgrade{
					Remediation: &v2beta2.UpgradeRemediation{
						// Flux controller might fail before cert-manager creates certificate, because
						// the default `retires` value is set to 0, it will not fail the HelmRelease resource
						// installation or upgrade. To make CI test run less flaky allow at most 5 retires.
						Retries: 5,
					},
				},
			},
			// Any empty structs are to make setting them more ergonomic
			// without having to worry about nil pointers.
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Config: &redpandav1alpha2.Config{},
				External: &redpandav1alpha2.External{
					// Disable NodePort creation to stop broken tests from blocking others due to port conflicts.
					Enabled: ptr.To(false),
				},
				Image: &redpandav1alpha2.RedpandaImage{
					Repository: ptr.To("redpandadata/redpanda"), // Use docker.io to make caching easier and to not inflate our own metrics.
				},
				Console: &redpandav1alpha2.RedpandaConsole{
					Enabled: ptr.To(false), // Speed up most cases by not enabling console to start.
				},
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(1), // Speed up tests ever so slightly.
					PodAntiAffinity: &redpandav1alpha2.PodAntiAffinity{
						// Disable the default "hard" affinity so we can
						// schedule multiple redpanda Pods on a single
						// kubernetes node. Useful for tests that require > 3
						// brokers.
						Type: ptr.To("soft"),
					},
					// Speeds up managed decommission tests. Decommissioned
					// nodes will take the entirety of
					// TerminationGracePeriodSeconds as the pre-stop hook
					// doesn't account for decommissioned nodes.
					TerminationGracePeriodSeconds: ptr.To(10),
				},
				Resources: &redpandav1alpha2.Resources{
					CPU: &redpandav1alpha2.CPU{
						// Inform redpanda/seastar that it's not going to get
						// all the resources it's promised.
						Overprovisioned: ptr.To(true),
					},
				},
			},
		},
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

	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}))
}

func (s *RedpandaControllerSuite) applyAndWait(objs ...client.Object) {
	s.applyAndWaitFor(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *redpandav1alpha2.Redpanda:
			ready := apimeta.IsStatusConditionTrue(obj.Status.Conditions, "Ready")
			upToDate := obj.Generation != 0 && obj.Generation == obj.Status.ObservedGeneration
			return upToDate && ready

		case *corev1.Secret, *corev1.ConfigMap, *corev1.ServiceAccount,
			*rbacv1.ClusterRole, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRoleBinding:
			return true

		default:
			s.T().Fatalf("unhandled object %T in applyAndWait", obj)
			panic("unreachable")
		}
	}, objs...)
}

func (s *RedpandaControllerSuite) applyAndWaitFor(cond func(client.Object) bool, objs ...client.Object) {
	for _, obj := range objs {
		gvk, err := s.client.GroupVersionKindFor(obj)
		s.NoError(err)

		obj.SetManagedFields(nil)
		obj.GetObjectKind().SetGroupVersionKind(gvk)

		s.Require().NoError(s.client.Patch(s.ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("tests")))
	}

	for _, obj := range objs {
		s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
			if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				return false, err
			}

			if cond(obj) {
				return true, nil
			}

			s.T().Logf("waiting for %T %q to be ready", obj, obj.GetName())
			return false, nil
		}))
	}
}

func (s *RedpandaControllerSuite) waitFor(obj client.Object, cond func(client.Object) bool) {
	s.NoError(wait.PollUntilContextTimeout(s.ctx, 5*time.Second, 5*time.Minute, false, func(ctx context.Context) (done bool, err error) {
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return false, err
		}

		if cond(obj) {
			return true, nil
		}

		s.T().Logf("waiting for %T %q to be ready", obj, obj.GetName())
		return false, nil
	}))
}

func TestPostInstallUpgradeJobIndex(t *testing.T) {
	dot, err := redpandachart.Chart.Dot(kube.Config{}, helmette.Release{}, map[string]any{})
	require.NoError(t, err)

	job := redpandachart.PostInstallUpgradeJob(dot)

	// Assert that index 0 is the envsubst container as that's what
	// `clusterConfigfor` utilizes.
	require.Equal(t, "bootstrap-yaml-envsubst", job.Spec.Template.Spec.InitContainers[0].Name)
}

func TestIsFluxEnabled(t *testing.T) {
	for _, tc := range []struct {
		expected          bool
		expectedSuspended bool
		useFluxCRD        *bool
		forceDefluxed     bool
	}{
		{true, false, ptr.To(true), false},
		{false, true, ptr.To(false), false},
		{true, false, nil, false},
		{true, false, ptr.To(true), true},
		{false, true, ptr.To(false), true},
		{false, true, nil, true},
	} {
		r := redpanda.RedpandaReconciler{DefaultDisableFlux: tc.forceDefluxed}
		assert.Equal(t, tc.expected, r.IsFluxEnabled(tc.useFluxCRD))
		// When it comes for helmrepository and helmrelease they should be suspended
		assert.Equal(t, tc.expectedSuspended, !r.IsFluxEnabled(tc.useFluxCRD))
	}
}

func TestHelmRepositoryURL(t *testing.T) {
	for _, tc := range []struct {
		helmRepoURL string
	}{
		{""},
		{"http://some-url.com/"},
		{"address-that-can-be-resolved"},
	} {
		r := redpanda.RedpandaReconciler{HelmRepositoryURL: tc.helmRepoURL}
		rp := &redpandav1alpha2.Redpanda{
			ObjectMeta: metav1.ObjectMeta{
				Name: "Redpanda-resource",
			},
			Spec: redpandav1alpha2.RedpandaSpec{
				ChartRef: redpandav1alpha2.ChartRef{
					ChartVersion: "5.x.x",
				},
				ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{},
			},
		}
		repo := r.HelmRepositoryFromTemplate(rp)
		assert.Equal(t, tc.helmRepoURL, repo.Spec.URL)
	}
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

		rules := role.Rules
		if !isNamespaced(typ) {
			rules = clusterRole.Rules
		}

		group := gvk.Group
		kind := pluralize(gvk.Kind)

		idx := slices.IndexFunc(rules, func(rule rbacv1.PolicyRule) bool {
			return slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, kind)
		})

		require.NotEqual(t, -1, idx, "missing rules for %s %s", gvk.Group, kind)
		require.EqualValues(t, expectedVerbs, rules[idx].Verbs, "incorrect verbs for %s %s", gvk.Group, kind)
	}
}

func isNamespaced(obj client.Object) bool {
	switch obj.(type) {
	case *corev1.Namespace, *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
		return false
	default:
		return true
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
