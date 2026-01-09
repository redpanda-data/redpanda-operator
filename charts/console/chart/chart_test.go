// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/redpanda-data/common-go/kube"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/helm/helmtest"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

func TestChartYAML(t *testing.T) {
	var images []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(Chart.Metadata().Annotations["artifacthub.io/images"]), &images))

	assert.Equal(t, []map[string]any{
		{
			"name":  "console",
			"image": "docker.redpanda.com/redpandadata/console:" + Chart.Metadata().AppVersion,
		},
	}, images)

	assert.Equal(t, console.ChartName, Chart.Metadata().Name)
	assert.Equal(t, console.AppVersion, Chart.Metadata().AppVersion)
}

// TestValues asserts that the chart's values.yaml file can be losslessly
// loaded into our type [Values] struct.
// NB: values.yaml should round trip through [Values], not [PartialValues], as
// [Values]'s omitempty tags are models after values.yaml.
func TestValues(t *testing.T) {
	var typedValues Values
	var unstructuredValues map[string]any

	require.NoError(t, yaml.Unmarshal(DefaultValuesYAML, &typedValues))
	require.NoError(t, yaml.Unmarshal(DefaultValuesYAML, &unstructuredValues))

	typedValuesJSON, err := json.Marshal(typedValues)
	require.NoError(t, err)

	unstructuredValuesJSON, err := json.Marshal(unstructuredValues)
	require.NoError(t, err)

	require.JSONEq(t, string(unstructuredValuesJSON), string(typedValuesJSON))
}

func TestTemplate(t *testing.T) {
	ctx := testutil.Context(t)
	client, err := helm.New(helm.Options{ConfigHome: testutil.TempDir(t)})
	require.NoError(t, err)

	casesArchive, err := txtar.ParseFile("testdata/template-cases.txtar")
	require.NoError(t, err)

	generatedCasesArchive, err := txtar.ParseFile("testdata/template-cases-generated.txtar")
	require.NoError(t, err)

	goldens := testutil.NewTxTar(t, "testdata/template-cases.golden.txtar")

	for _, tc := range append(casesArchive.Files, generatedCasesArchive.Files...) {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			var values PartialValues
			require.NoError(t, yaml.Unmarshal(tc.Data, &values))

			out, err := client.Template(ctx, ".", helm.TemplateOptions{
				Name:      "console",
				Namespace: "test-namespace",
				Values:    values,
				Set: []string{
					// jwtSigningKey defaults to a random string. Can't have that
					// in snapshot testing so set it to a static value.
					"secret.authentication.jwtSigningKey=SECRETKEY",
				},
			})
			require.NoError(t, err)

			objs, err := kube.DecodeYAML(out, console.Scheme)
			require.NoError(t, err)

			for _, obj := range objs {
				assert.Equalf(t, "test-namespace", obj.GetNamespace(), "%T %q did not have namespace correctly set", obj, obj.GetName())
			}

			goldens.AssertGolden(t, testutil.YAML, fmt.Sprintf("testdata/%s.yaml.golden", tc.Name), out)
		})
	}
}

// TestGenerateCases is not a test case (sorry) but a test case generator for
// the console chart.
func TestGenerateCases(t *testing.T) {
	// Nasty hack to avoid making a main function somewhere. Sorry not sorry.
	if !slices.Contains(os.Args, fmt.Sprintf("-test.run=%s", t.Name())) {
		t.Skipf("%s will only run if explicitly specified (-run %q)", t.Name(), t.Name())
	}

	// Makes strings easier to read.
	asciiStrs := func(s *string, c fuzz.Continue) {
		const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		var x []byte
		for i := 0; i < c.Intn(25); i++ {
			x = append(x, alphabet[c.Intn(len(alphabet))])
		}
		*s = string(x)
	}
	smallInts := func(s *int, c fuzz.Continue) {
		*s = c.Intn(501)
	}

	fuzzer := fuzz.New().NumElements(0, 3).SkipFieldsWithPattern(
		regexp.MustCompile("^(SELinuxOptions|WindowsOptions|SeccompProfile|TCPSocket|HTTPHeaders|VolumeSource)$"),
	).Funcs(
		asciiStrs,
		smallInts,
		func(t *corev1.ServiceType, c fuzz.Continue) {
			types := []corev1.ServiceType{
				corev1.ServiceTypeClusterIP,
				corev1.ServiceTypeExternalName,
				corev1.ServiceTypeNodePort,
				corev1.ServiceTypeLoadBalancer,
			}
			*t = types[c.Intn(len(types))]
		},
		func(s *corev1.ResourceName, c fuzz.Continue) { asciiStrs((*string)(s), c) },
		func(_ *any, c fuzz.Continue) {},
		func(_ *[]corev1.ResourceClaim, c fuzz.Continue) {},
		func(_ *[]metav1.ManagedFieldsEntry, c fuzz.Continue) {},
	)

	schema, err := jsonschema.CompileString("", string(ValuesSchemaJSON))
	require.NoError(t, err)

	nilChance := float64(0.8)

	files := make([]txtar.File, 0, 50)
	for i := 0; i < 50; i++ {
		// Every 5 iterations, decrease nil chance to ensure that we're biased
		// towards exploring most cases.
		if i%5 == 0 && nilChance > .1 {
			nilChance -= .1
		}

		var values PartialValues
		fuzzer.NilChance(nilChance).Fuzz(&values)

		out, err := yaml.Marshal(values)
		require.NoError(t, err)

		merged, err := helm.MergeYAMLValues(DefaultValuesYAML, out)
		require.NoError(t, err)

		// Ensure that our generated values comply with the schema set by the chart.
		if err := schema.Validate(merged); err != nil {
			t.Logf("Generated invalid values; trying again...\n%v", err)
			i--
			continue
		}

		files = append(files, txtar.File{
			Name: fmt.Sprintf("case-%03d", i),
			Data: out,
		})
	}

	archive := txtar.Format(&txtar.Archive{
		Comment: []byte(fmt.Sprintf(`Generated by %s`, t.Name())),
		Files:   files,
	})

	require.NoError(t, os.WriteFile("testdata/template-cases-generated.txtar", archive, 0o644))
}

func TestGoHelmEquivalence(t *testing.T) {
	client, err := helm.New(helm.Options{ConfigHome: testutil.TempDir(t)})
	require.NoError(t, err)

	values := PartialValues{
		Tests: &PartialEnableable{
			Enabled: ptr.To(false),
		},
		PartialRenderValues: console.PartialRenderValues{
			Secret: &console.PartialSecretConfig{
				Authentication: &console.PartialAuthenticationSecrets{
					JWTSigningKey: ptr.To("SECRET"),
				},
			},
			Ingress: &console.PartialIngressConfig{
				Enabled: ptr.To(true),
			},
		},
	}

	goObjs, err := Chart.Render(nil, helmette.Release{
		Name:      "gotohelm",
		Namespace: "mynamespace",
		Service:   "Helm",
	}, values)
	require.NoError(t, err)

	rendered, err := client.Template(context.Background(), ".", helm.TemplateOptions{
		Name:      "gotohelm",
		Namespace: "mynamespace",
		Values:    values,
	})
	require.NoError(t, err)

	helmObjs, err := kube.DecodeYAML(rendered, console.Scheme)
	require.NoError(t, err)

	slices.SortStableFunc(helmObjs, func(a, b kube.Object) int {
		aStr := fmt.Sprintf("%s/%s/%s", a.GetObjectKind().GroupVersionKind().String(), a.GetNamespace(), a.GetName())
		bStr := fmt.Sprintf("%s/%s/%s", b.GetObjectKind().GroupVersionKind().String(), b.GetNamespace(), b.GetName())
		return strings.Compare(aStr, bStr)
	})

	slices.SortStableFunc(goObjs, func(a, b kube.Object) int {
		aStr := fmt.Sprintf("%s/%s/%s", a.GetObjectKind().GroupVersionKind().String(), a.GetNamespace(), a.GetName())
		bStr := fmt.Sprintf("%s/%s/%s", b.GetObjectKind().GroupVersionKind().String(), b.GetNamespace(), b.GetName())
		return strings.Compare(aStr, bStr)
	})

	assert.Equal(t, len(helmObjs), len(goObjs))

	// Iterate and compare instead of a single comparison for better error
	// messages. Some divergences will fail an Equal check on slices but not
	// report which element(s) aren't equal.
	for i := range helmObjs {
		assert.Equal(t, helmObjs[i], goObjs[i])
	}
}

func TestSecrets(t *testing.T) {
	values := PartialValues{
		PartialRenderValues: console.PartialRenderValues{
			Secret: &console.PartialSecretConfig{
				Create: ptr.To(true),
				Kafka: &console.PartialKafkaSecrets{
					SASLPassword: ptr.To("test-sasl-password"),
				},
				SchemaRegistry: &console.PartialSchemaRegistrySecrets{
					Password: ptr.To("test-schema-registry-password"),
				},
				Authentication: &console.PartialAuthenticationSecrets{
					JWTSigningKey: ptr.To("test-jwt-signing-key"),
				},
				Redpanda: &console.PartialRedpandaSecrets{
					AdminAPI: &console.PartialRedpandaAdminAPISecrets{
						Password: ptr.To("test-redpanda-admin-api-password"),
					},
				},
				License: ptr.To("console-license-secret-content"),
			},
		},
		Tests: &PartialEnableable{
			Enabled: ptr.To(false),
		},
	}

	goObjs, err := Chart.Render(nil, helmette.Release{
		Name:      "gotohelm",
		Namespace: "mynamespace",
		Service:   "Helm",
	}, values)
	require.NoError(t, err)

	secrets := make(map[string]*corev1.Secret)
	var deploy *appsv1.Deployment
	for _, obj := range goObjs {
		switch obj := obj.(type) {
		case *corev1.Secret:
			secrets[obj.Name] = obj
		case *appsv1.Deployment:
			deploy = obj
		}
	}

	consoleSecret, ok := secrets["gotohelm-console"]
	assert.True(t, ok, "Failed to get console secret")
	require.NotNil(t, consoleSecret, "Failed to get console secret")

	assert.Equal(t, "test-sasl-password", string(consoleSecret.StringData["kafka-sasl-password"]))
	assert.Equal(t, "test-schema-registry-password", string(consoleSecret.StringData["schema-registry-password"]))
	assert.Equal(t, "test-jwt-signing-key", string(consoleSecret.StringData["authentication-jwt-signingkey"]))
	assert.Equal(t, "test-redpanda-admin-api-password", string(consoleSecret.StringData["redpanda-admin-api-password"]))

	require.NotNil(t, deploy, "Failed to get console deployment")

	for _, container := range deploy.Spec.Template.Spec.Containers {
		envs := container.Env
		envMap := make(map[string]string)
		for _, env := range envs {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				assert.Equal(t, "gotohelm-console", env.ValueFrom.SecretKeyRef.Name)
				envMap[env.Name] = env.ValueFrom.SecretKeyRef.Key
			}
		}

		assert.Equal(t, "kafka-sasl-password", envMap["KAFKA_SASL_PASSWORD"])
		assert.Equal(t, "schema-registry-password", envMap["SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD"])
		assert.Equal(t, "authentication-jwt-signingkey", envMap["AUTHENTICATION_JWTSIGNINGKEY"])
		assert.Equal(t, "redpanda-admin-api-password", envMap["REDPANDA_ADMINAPI_PASSWORD"])
		assert.Equal(t, "license", envMap["LICENSE"])
	}
}

func TestAppVersion(t *testing.T) {
	const project = "charts/console"

	output, err := exec.Command("changie", "latest", "-j", project).CombinedOutput()
	require.NoError(t, err)

	// Trim the project prefix to just get `x.y.z`.
	expected := string(output[len(project+"/v"):])
	actual := Chart.Metadata().Version

	require.Equalf(t, expected, actual, "Chart.yaml's version should be %q; got %q\nDid you forget to update Chart.yaml before minting a release?\nMake sure to bump appVersion as well!", expected, actual)
}

func TestIntegrationChart(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	consoleChart := "."

	h := helmtest.Setup(t)

	t.Run("basic test", func(t *testing.T) {
		ctx := testutil.Context(t)

		env := h.Namespaced(t)

		partial := PartialValues{
			PartialRenderValues: console.PartialRenderValues{
				Image: &console.PartialImage{
					Repository: ptr.To("redpandadata/console-unstable"),
					Tag:        ptr.To("master-a4cf9be"),
				},
				Config: map[string]any{
					"kafka": map[string]any{
						"brokers": []string{"some-broker-address:9092"}, // Console and this test does not care whether we have a real broker
						"startup": map[string]any{
							"establishConnectionEagerly": false,
						},
					},
				},
			},
		}

		consoleRelease := env.Install(ctx, consoleChart, helm.InstallOptions{
			Values:       partial,
			Namespace:    env.Namespace(),
			GenerateName: true,
		})

		dot, err := Chart.Dot(nil, helmette.Release{
			Name:      consoleRelease.Name,
			Namespace: consoleRelease.Namespace,
		}, partial)
		require.NoError(t, err)

		client, err := NewClient(ctx, env.Ctl(), dot)
		assert.NoError(t, err, "Failed to create console client")

		body, err := client.GetDebugVars(ctx)

		assert.NoError(t, err, "Failed to get debug vars")
		assert.Contains(t, body, "--config.filepath=/etc/console/configs/config.yaml")
	})
}
