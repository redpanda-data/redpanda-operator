// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources_test

import (
	"context"
	"crypto/tls"
	"log"
	"os"
	"testing"
	"time"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/internal/controller"
	"github.com/redpanda-data/redpanda-operator/operator/internal/testutils"
	adminutils "github.com/redpanda-data/redpanda-operator/operator/pkg/admin"
	res "github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

var c client.Client

func TestMain(m *testing.M) {
	var err error

	testEnv := &testutils.RedpandaTestEnv{}

	cfg, err := testEnv.StartRedpandaTestEnv(false)
	if err != nil {
		log.Fatal(err)
	}

	clientOptions := client.Options{Scheme: controller.UnifiedScheme}

	c, err = client.New(cfg, clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "archival",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"archival": []byte("XXX"),
		},
	}
	if err := c.Create(context.TODO(), &secret); err != nil {
		log.Fatal(err)
	}

	exitCode := m.Run()
	err = testEnv.Stop()
	if err != nil {
		log.Fatal(err)
	}

	os.Exit(exitCode)
}

func TestEnsure_StatefulSet(t *testing.T) {
	cluster := pandaCluster()
	cluster = cluster.DeepCopy()
	cluster.Name = "ensure-integration-cluster"
	err := c.Create(context.Background(), cluster)
	require.NoError(t, err)

	cfg, err := res.CreateConfiguration(context.TODO(), c, nil, cluster, "cluster.local", types.NamespacedName{Name: "test", Namespace: "test"}, types.NamespacedName{Name: "test", Namespace: "test"}, types.NamespacedName{Name: "test", Namespace: "test"}, TestBrokerTLSConfigProvider{})
	require.NoError(t, err)
	sts := res.NewStatefulSet(
		c,
		cluster,
		controller.UnifiedScheme,
		"cluster.local",
		"servicename",
		types.NamespacedName{Name: "test", Namespace: "test"},
		TestStatefulsetTLSVolumeProvider{},
		TestAdminTLSConfigProvider{},
		"",
		res.ConfiguratorSettings{
			ConfiguratorBaseImage: "redpanda-data/redpanda-operator",
			ConfiguratorTag:       "latest",
			ImagePullPolicy:       "Always",
		},
		cfg,
		adminutils.NewNodePoolInternalAdminAPI,
		nil,
		time.Second,
		ctrl.Log.WithName("test"),
		0,
		vectorizedv1alpha1.NodePoolSpecWithDeleted{NodePoolSpec: cluster.Spec.NodePools[0]},
		true,
	)

	err = sts.Ensure(context.Background())
	assert.NoError(t, err)

	actual := &appsv1.StatefulSet{}
	err = c.Get(context.Background(), sts.Key(), actual)
	assert.NoError(t, err)
	originalResourceVersion := actual.ResourceVersion

	// calling ensure for second time to see the resource does not get updated
	err = sts.Ensure(context.Background())
	assert.NoError(t, err)

	err = c.Get(context.Background(), sts.Key(), actual)
	assert.NoError(t, err)
	if actual.ResourceVersion != originalResourceVersion {
		t.Fatalf("second ensure: expecting version %s but got %s", originalResourceVersion, actual.GetResourceVersion())
	}
}

func TestEnsure_ConfigMap(t *testing.T) {
	cluster := pandaCluster()
	cluster = cluster.DeepCopy()
	cluster.Name = "ensure-integration-cm-cluster"
	assert.NoError(t, c.Create(context.Background(), cluster))

	mustCfgAndCm := func() *res.ConfigMapResource {
		// CombinedCfg is immutable once it's been templated,
		// so we have to create new values here.
		cfg, err := res.CreateConfiguration(context.TODO(), c, nil, cluster, "cluster.local", types.NamespacedName{Name: "test", Namespace: "test"}, types.NamespacedName{Name: "test", Namespace: "test"}, types.NamespacedName{Namespace: "namespace", Name: "test"}, TestBrokerTLSConfigProvider{})
		require.NoError(t, err)
		cm := res.NewConfigMap(
			c,
			cluster,
			controller.UnifiedScheme,
			cfg,
			ctrl.Log.WithName("test"))

		err = cm.Ensure(context.Background())
		assert.NoError(t, err)

		return cm
	}

	cm := mustCfgAndCm()
	actual := &corev1.ConfigMap{}
	err := c.Get(context.Background(), cm.Key(), actual)
	assert.NoError(t, err)
	originalResourceVersion := actual.ResourceVersion

	data := mustUnmarshal[map[string]string](t, actual.Data[clusterconfiguration.BootstrapTemplateFile])
	if data["auto_create_topics_enabled"] != "false" {
		t.Fatalf("expecting configmap containing 'auto_create_topics_enabled: false' but got %v", data)
	}

	// calling ensure for second time to see the resource does not get updated
	cm = mustCfgAndCm()
	err = c.Get(context.Background(), cm.Key(), actual)
	assert.NoError(t, err)
	if actual.ResourceVersion != originalResourceVersion {
		t.Fatalf("second ensure: expecting version %s but got %s", originalResourceVersion, actual.GetResourceVersion())
	}

	// verify the update patches the config.
	// CombinedCfg is immutable once it's been written out, so we have to create a new value here.
	cluster.Spec.Configuration.KafkaAPI[0].Port = 1111
	cluster.Spec.Configuration.KafkaAPI[0].TLS.Enabled = true
	cm = mustCfgAndCm()

	err = c.Get(context.Background(), cm.Key(), actual)
	assert.NoError(t, err)
	if actual.ResourceVersion == originalResourceVersion {
		t.Fatalf("expecting version to get updated after resource update but is %s", originalResourceVersion)
	}
	rpYaml := mustUnmarshal[config.RedpandaYaml](t, actual.Data[clusterconfiguration.RedpandaYamlTemplateFile])
	assert.NotEmpty(t, rpYaml.Redpanda.KafkaAPITLS[0].CertFile, "expecting configmap updated")
	assert.Equal(t, 1111, rpYaml.Redpanda.KafkaAPI[0].Port, "expecting configmap updated")
}

func mustUnmarshal[T any](t *testing.T, entry string) T {
	var value T
	require.NoError(t, yaml.Unmarshal([]byte(entry), &value))
	return value
}

//nolint:funlen // the subtests might causes linter to complain
func TestEnsure_HeadlessService(t *testing.T) {
	t.Run("create-headless-service", func(t *testing.T) {
		cluster := pandaCluster()
		cluster.Name = "create-headles-service"

		hsvc := res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 123},
			},
			ctrl.Log.WithName("test"))

		err := hsvc.Ensure(context.Background())
		assert.NoError(t, err)

		actual := &corev1.Service{}
		err = c.Get(context.Background(), hsvc.Key(), actual)
		assert.NoError(t, err)
		assert.Equal(t, int32(123), actual.Spec.Ports[0].Port)
	})

	t.Run("create headless service idempotency", func(t *testing.T) {
		cluster := pandaCluster()
		cluster.Name = "create-headles-service-idempotency"

		hsvc := res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 123},
			},
			ctrl.Log.WithName("test"))

		err := hsvc.Ensure(context.Background())
		assert.NoError(t, err)

		actual := &corev1.Service{}
		err = c.Get(context.Background(), hsvc.Key(), actual)
		assert.NoError(t, err)
		originalResourceVersion := actual.ResourceVersion

		err = hsvc.Ensure(context.Background())
		assert.NoError(t, err)

		err = c.Get(context.Background(), hsvc.Key(), actual)
		assert.NoError(t, err)
		assert.Equal(t, originalResourceVersion, actual.ResourceVersion)
	})

	t.Run("updating headless service", func(t *testing.T) {
		cluster := pandaCluster()
		cluster.Name = "update-headles-service"

		hsvc := res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 123},
			},
			ctrl.Log.WithName("test"))

		err := hsvc.Ensure(context.Background())
		assert.NoError(t, err)

		actual := &corev1.Service{}
		err = c.Get(context.Background(), hsvc.Key(), actual)
		assert.NoError(t, err)
		originalResourceVersion := actual.ResourceVersion

		hsvc = res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 1111},
			},
			ctrl.Log.WithName("test"))

		err = hsvc.Ensure(context.Background())
		assert.NoError(t, err)

		err = c.Get(context.Background(), hsvc.Key(), actual)
		assert.NoError(t, err)
		assert.NotEqual(t, originalResourceVersion, actual.ResourceVersion)
		assert.Equal(t, int32(1111), actual.Spec.Ports[0].Port)
	})

	t.Run("HeadlessServiceFQDN with trailing dot", func(t *testing.T) {
		cluster := pandaCluster()
		cluster.Name = "trailing-dot-headles-service"
		cluster.Namespace = "some-namespace"

		hsvc := res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 123},
			},
			ctrl.Log.WithName("test"))

		fqdn := hsvc.HeadlessServiceFQDN("some.domain")
		assert.Equal(t, "trailing-dot-headles-service.some-namespace.svc.some.domain.", fqdn)
	})

	t.Run("HeadlessServiceFQDN without trailing dot", func(t *testing.T) {
		cluster := pandaCluster()
		cluster.Name = "without-trailing-dot-headles-service"
		cluster.Namespace = "different-namespace"
		cluster.Spec.DNSTrailingDotDisabled = true

		hsvc := res.NewHeadlessService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Port: 123},
			},
			ctrl.Log.WithName("test"))

		fqdn := hsvc.HeadlessServiceFQDN("some.domain")
		assert.Equal(t, "without-trailing-dot-headles-service.different-namespace.svc.some.domain", fqdn)
	})
}

func TestEnsure_NodePortService(t *testing.T) {
	cluster := pandaCluster()
	cluster = cluster.DeepCopy()
	cluster.Spec.Configuration.KafkaAPI = append(cluster.Spec.Configuration.KafkaAPI,
		vectorizedv1alpha1.KafkaAPI{External: vectorizedv1alpha1.ExternalConnectivityConfig{Enabled: true}})
	cluster.Name = "ensure-integration-np-cluster"

	npsvc := res.NewNodePortService(
		c,
		cluster,
		controller.UnifiedScheme,
		[]res.NamedServiceNodePort{
			{NamedServicePort: res.NamedServicePort{Port: 123}, GenerateNodePort: true},
		},
		ctrl.Log.WithName("test"))

	err := npsvc.Ensure(context.Background())
	assert.NoError(t, err)

	actual := &corev1.Service{}
	err = c.Get(context.Background(), npsvc.Key(), actual)
	assert.NoError(t, err)
	originalResourceVersion := actual.ResourceVersion

	// calling ensure for second time to see the resource does not get updated
	err = npsvc.Ensure(context.Background())
	assert.NoError(t, err)

	err = c.Get(context.Background(), npsvc.Key(), actual)
	assert.NoError(t, err)
	if actual.ResourceVersion != originalResourceVersion {
		t.Fatalf("second ensure: expecting version %s but got %s", originalResourceVersion, actual.GetResourceVersion())
	}

	// verify the update patches the config

	// TODO this has to recreate the resource because the ports are passed from
	// outside. Once we refactor it to a point where ports are derived from CR
	// as it should, this test should be adjusted
	npsvc = res.NewNodePortService(
		c,
		cluster,
		controller.UnifiedScheme,
		[]res.NamedServiceNodePort{
			{NamedServicePort: res.NamedServicePort{Port: 1111}, GenerateNodePort: true},
		},
		ctrl.Log.WithName("test"))

	err = npsvc.Ensure(context.Background())
	assert.NoError(t, err)

	err = c.Get(context.Background(), npsvc.Key(), actual)
	assert.NoError(t, err)
	if actual.ResourceVersion == originalResourceVersion {
		t.Fatalf("expecting version to get updated after resource update but is %s", originalResourceVersion)
	}
	port := actual.Spec.Ports[0].Port
	if port != 1111 {
		t.Fatalf("expecting configmap updated but got %d", port)
	}
}

func TestEnsure_LoadbalancerService(t *testing.T) {
	t.Run("create-loadbalancer-service", func(t *testing.T) {
		cluster := pandaCluster()
		cluster = cluster.DeepCopy()
		cluster.Spec.Configuration.KafkaAPI = append(cluster.Spec.Configuration.KafkaAPI,
			[]vectorizedv1alpha1.KafkaAPI{
				{
					Port: 1111,
				},
				{
					External: vectorizedv1alpha1.ExternalConnectivityConfig{
						Enabled: true,
						Bootstrap: &vectorizedv1alpha1.LoadBalancerConfig{
							Annotations: map[string]string{"key1": "val1"},
							Port:        2222,
						},
					},
				},
			}...)
		cluster.Name = "ensure-integration-lb-cluster"

		lb := res.NewLoadBalancerService(
			c,
			cluster,
			controller.UnifiedScheme,
			[]res.NamedServicePort{
				{Name: "kafka-external-bootstrap", Port: 2222, TargetPort: 1112},
			},
			true,
			ctrl.Log.WithName("test"))

		err := lb.Ensure(context.Background())
		assert.NoError(t, err)

		actual := &corev1.Service{}
		err = c.Get(context.Background(), lb.Key(), actual)
		assert.NoError(t, err)

		assert.Equal(t, "ensure-integration-lb-cluster-lb-bootstrap", actual.Name)

		_, annotationExists := actual.Annotations["key1"]
		assert.True(t, annotationExists)
		assert.Equal(t, "val1", actual.Annotations["key1"])

		assert.True(t, len(actual.Spec.Ports) == 1)
		assert.Equal(t, int32(2222), actual.Spec.Ports[0].Port)
		assert.Equal(t, 1112, actual.Spec.Ports[0].TargetPort.IntValue())
		assert.Equal(t, corev1.ProtocolTCP, actual.Spec.Ports[0].Protocol)
		assert.Equal(t, "kafka-external-bootstrap", actual.Spec.Ports[0].Name)
	})
}

//nolint:funlen // more cases are welcome
func TestEnsure_Ingress(t *testing.T) {
	cluster := pandaCluster()
	cluster = cluster.DeepCopy()
	err := c.Create(context.Background(), cluster)
	require.NoError(t, err)
	falseVar := false
	emptyString := ""

	cases := []struct {
		name                string
		configs             []*vectorizedv1alpha1.IngressConfig
		internalAnnotations map[string]string
		defaultEndpoint     string
		customSubdomain     *string
		expectNoIngress     bool
		expectHost          string
		expectAnnotations   map[string]string
	}{
		{
			name:       "no user config",
			configs:    []*vectorizedv1alpha1.IngressConfig{nil},
			expectHost: "external.domain",
		},
		{
			name:       "empty user config",
			configs:    []*vectorizedv1alpha1.IngressConfig{{}},
			expectHost: "external.domain",
		},
		{
			name: "endpoint in user config",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Endpoint: "pp-rnd",
				},
			},
			expectHost: "pp-rnd.external.domain",
		},
		{
			name:            "using default endpoint",
			configs:         []*vectorizedv1alpha1.IngressConfig{nil},
			defaultEndpoint: "console",
			expectHost:      "console.external.domain",
		},
		{
			name: "user endpoint override default endpoint",
			configs: []*vectorizedv1alpha1.IngressConfig{
				nil,
				{
					Endpoint: "override",
				},
			},
			defaultEndpoint: "console",
			expectHost:      "override.external.domain",
		},
		{
			name: "ingress explicitly disabled",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Enabled: &falseVar,
				},
			},
			expectNoIngress: true,
		},
		{
			name: "no subdomain on ingress",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Endpoint: "anything",
				},
			},
			customSubdomain: &emptyString,
			expectNoIngress: true,
		},
		{
			name: "annotations in user config",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Endpoint: "pp-rnd",
					Annotations: map[string]string{
						"a": "b",
						"b": "c",
					},
				},
			},
			expectHost: "pp-rnd.external.domain",
			expectAnnotations: map[string]string{
				"a": "b",
				"b": "c",
			},
		},
		{
			name: "user annotations override",
			internalAnnotations: map[string]string{
				"a": "not-this",
				"c": "d",
			},
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Endpoint: "pp-rnd",
					Annotations: map[string]string{
						"a": "b",
						"b": "c",
					},
				},
			},
			expectHost: "pp-rnd.external.domain",
			expectAnnotations: map[string]string{
				"a": "b",
				"b": "c",
				"c": "d",
			},
		},
		{
			name: "create then delete twice",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Endpoint: "pp-rnd",
					Annotations: map[string]string{
						"a": "b",
						"b": "c",
					},
				},
				{
					Enabled: &falseVar,
				},
				{
					Enabled: &falseVar,
				},
			},
			expectNoIngress: true,
		},
		{
			name: "disable then enable and change",
			configs: []*vectorizedv1alpha1.IngressConfig{
				{
					Enabled: &falseVar,
				},
				{
					Endpoint: "xx",
					Annotations: map[string]string{
						"a": "xx",
					},
				},
				{
					Endpoint: "pp-rnd",
					Annotations: map[string]string{
						"a": "b",
					},
				},
			},
			expectHost: "pp-rnd.external.domain",
			expectAnnotations: map[string]string{
				"a": "b",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, len(tc.configs) > 0)
			var ingress *res.IngressResource
			for _, conf := range tc.configs {
				subdomain := "external.domain"
				if tc.customSubdomain != nil {
					subdomain = *tc.customSubdomain
				}
				ingress = res.NewIngress(
					c,
					cluster,
					controller.UnifiedScheme,
					subdomain,
					"svcname",
					"svcport",
					ctrl.Log.WithName("test"),
				).WithAnnotations(tc.internalAnnotations).
					WithUserConfig(conf).
					WithDefaultEndpoint(tc.defaultEndpoint)

				err = ingress.Ensure(context.Background())
				require.NoError(t, err)
			}

			actual := networkingv1.Ingress{}
			err = c.Get(context.Background(), ingress.Key(), &actual)
			if tc.expectNoIngress {
				require.Error(t, err)
				assert.True(t, k8serrors.IsNotFound(err))
				return
			}

			require.NoError(t, err)
			defer c.Delete(context.Background(), &actual) //nolint:errcheck // best effort

			require.Len(t, actual.Spec.Rules, 1)
			require.Equal(t, tc.expectHost, actual.Spec.Rules[0].Host)

			for k, v := range tc.expectAnnotations {
				assert.Equal(t, v, actual.Annotations[k])
			}
		})
	}
}

type TestStatefulsetTLSVolumeProvider struct{}

func (TestStatefulsetTLSVolumeProvider) Volumes() (
	[]corev1.Volume,
	[]corev1.VolumeMount,
) {
	return []corev1.Volume{}, []corev1.VolumeMount{}
}

type TestAdminTLSConfigProvider struct{}

func (TestAdminTLSConfigProvider) GetTLSConfig(
	ctx context.Context, k8sClient client.Reader,
) (*tls.Config, error) {
	return nil, nil
}
