// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

type ChartYAML struct {
	Version     string            `json:"version"`
	AppVersion  string            `json:"appVersion"`
	Annotations map[string]string `json:"annotations"`
}

func TestChartYaml(t *testing.T) {
	const changieCmd = "changie latest -j operator"
	const operatorRepo = "docker.redpanda.com/redpandadata/redpanda-operator"

	out, err := exec.Command("sh", "-c", changieCmd).CombinedOutput()
	require.NoError(t, err)

	expectedVersion := string(out[len("operator/"):])

	assert.Equal(t, expectedVersion, Chart.Metadata().Version, "Chart.yaml's version should match `%s: %s", changieCmd, expectedVersion)

	assert.Equal(t, Chart.Metadata().AppVersion, Chart.Metadata().Version, "The Operator and its chart are distributed as the same package. Any changes to appVersion should be made to version and visa vera.")

	assert.Contains(
		t,
		Chart.Metadata().Annotations["artifacthub.io/images"],
		fmt.Sprintf("%s:%s", operatorRepo, expectedVersion),
		"artifacthub.io/images should be in sync with .version",
	)
}

func TestRBACBindings(t *testing.T) {
	testCases := []struct {
		name   string
		values PartialValues
	}{
		{
			name:   "defaults",
			values: PartialValues{},
		},
		{
			name: "additional-controllers",
			values: PartialValues{
				RBAC: &PartialRBAC{
					CreateAdditionalControllerCRs: ptr.To(true),
				},
			},
		},
		{
			name: "rpk-debug-bundle",
			values: PartialValues{
				RBAC: &PartialRBAC{
					CreateRPKBundleCRs: ptr.To(true),
				},
			},
		},
		{
			name: "cluster-scope",
			values: PartialValues{
				Scope: ptr.To(Cluster),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			chartObjs, err := Chart.Render(nil, helmette.Release{
				Name: "operator",
			}, tc.values)
			require.NoError(t, err)

			var roles []string
			var clusterRoles []string

			var boundRoles []string
			var boundClusterRoles []string

			for _, obj := range chartObjs {
				switch obj := obj.(type) {
				case *rbacv1.Role:
					roles = append(roles, obj.Name)

				case *rbacv1.ClusterRole:
					clusterRoles = append(clusterRoles, obj.Name)

				case *rbacv1.ClusterRoleBinding:
					switch obj.RoleRef.Kind {
					case "Role":
						boundRoles = append(boundRoles, obj.RoleRef.Name)
					case "ClusterRole":
						boundClusterRoles = append(boundClusterRoles, obj.RoleRef.Name)
					default:
						t.Fatalf("unknown RoleRef.Kind: %q", obj.RoleRef.Kind)
					}

				case *rbacv1.RoleBinding:
					switch obj.RoleRef.Kind {
					case "Role":
						boundRoles = append(boundRoles, obj.RoleRef.Name)
					case "ClusterRole":
						boundClusterRoles = append(boundClusterRoles, obj.RoleRef.Name)
					default:
						t.Fatalf("unknown RoleRef.Kind: %q", obj.RoleRef.Kind)
					}
				}
			}

			for _, bound := range boundRoles {
				require.Contains(t, roles, bound, "found binding to non-existent Role")
			}

			for _, role := range roles {
				require.Contains(t, boundRoles, role, "found Role %q with no binding", role)
			}

			for _, bound := range boundClusterRoles {
				require.Contains(t, clusterRoles, bound, "found binding to non-existent ClusterRole")
			}

			for _, clusterRole := range clusterRoles {
				// Special case, skip over the metrics-reader ClusterRole as it's intentionally unbound.
				if strings.HasSuffix(clusterRole, "metrics-reader") {
					continue
				}
				require.Contains(t, boundClusterRoles, clusterRole, "found ClusterRole %q with no binding", clusterRole)
			}
		})
	}
}

func TestRBACIsSuperSetOfRedpanda(t *testing.T) {
	testCases := []struct {
		Name           string
		RedpandaValues redpanda.PartialValues
		OperatorValues PartialValues
	}{
		{
			Name: "defaults",
		},
		// TODO(chrisseto): Add more test cases once the RBAC conditions in the
		// redpanda chart have been corrected. They are currently a bit over
		// aggressive.
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			redpandaObjs, err := redpanda.Chart.Render(nil, helmette.Release{}, tc.RedpandaValues)
			require.NoError(t, err)

			operatorObjs, err := Chart.Render(nil, helmette.Release{}, tc.OperatorValues)
			require.NoError(t, err)

			redpandaClusterRoleRules, redpandaRoleRules := ExtractRules(redpandaObjs)
			operatorClusterRoleRules, operatorRoleRules := ExtractRules(operatorObjs)

			assertRulesSuperSet(t, operatorRoleRules, redpandaRoleRules)
			assertRulesSuperSet(t, operatorClusterRoleRules, redpandaClusterRoleRules)
		})
	}
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
			t.Parallel()

			var values PartialValues
			require.NoError(t, yaml.Unmarshal(tc.Data, &values))

			out, err := client.Template(ctx, ".", helm.TemplateOptions{
				Name:   "operator",
				Values: values,
				Set:    []string{},
			})
			require.NoError(t, err)
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
		func(p *corev1.PullPolicy, c fuzz.Continue) {
			policies := []corev1.PullPolicy{
				corev1.PullAlways,
				corev1.PullNever,
				corev1.PullIfNotPresent,
			}

			*p = policies[c.Intn(len(policies))]
		},
		func(r corev1.ResourceList, c fuzz.Continue) {
			r[corev1.ResourceCPU] = resource.MustParse(strconv.Itoa(c.Intn(1000)))
			r[corev1.ResourceMemory] = resource.MustParse(strconv.Itoa(c.Intn(1000)))
		},
		func(p *corev1.Probe, c fuzz.Continue) {
			p.InitialDelaySeconds = int32(c.Intn(1000))
			p.PeriodSeconds = int32(c.Intn(1000))
			p.TimeoutSeconds = int32(c.Intn(1000))
			p.SuccessThreshold = int32(c.Intn(1000))
			p.FailureThreshold = int32(c.Intn(1000))
			p.TerminationGracePeriodSeconds = ptr.To(int64(c.Intn(1000)))
		},
		func(p *corev1.PodFSGroupChangePolicy, c fuzz.Continue) {
			policies := []corev1.PodFSGroupChangePolicy{
				corev1.FSGroupChangeOnRootMismatch,
				corev1.FSGroupChangeAlways,
			}

			*p = policies[c.Intn(len(policies))]
		},
		func(s *intstr.IntOrString, c fuzz.Continue) {
			*s = intstr.FromInt32(c.Int31())
		},
		func(s *corev1.ResourceName, c fuzz.Continue) { asciiStrs((*string)(s), c) },
		func(_ *any, c fuzz.Continue) {},
		func(_ *[]corev1.ResourceClaim, c fuzz.Continue) {},
		func(_ *[]metav1.ManagedFieldsEntry, c fuzz.Continue) {},
	)

	schema, err := jsonschema.CompileString("", string(ValuesSchemaJSON))
	require.NoError(t, err)

	files := make([]txtar.File, 0, 100)
	for _, scope := range []OperatorScope{Namespace, Cluster} {
		nilChance := float64(0.8)
		for i := 0; i < 50; i++ {
			// Every 5 iterations, decrease nil chance to ensure that we're biased
			// towards exploring most cases.
			if i%5 == 0 && nilChance > .1 {
				nilChance -= .1
			}

			var values PartialValues
			fuzzer.NilChance(nilChance).Fuzz(&values)
			// Special case as fuzzer does not assign correctly scope
			values.Scope = &scope
			if scope == Cluster {
				values.Webhook = &PartialWebhook{Enabled: ptr.To(true)}
			} else {
				values.Webhook = &PartialWebhook{Enabled: ptr.To(false)}
			}
			makeSureTagIsNotEmptyString(values, fuzzer)

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

			index := i
			if scope == Cluster {
				index += 50
			}

			files = append(files, txtar.File{
				Name: fmt.Sprintf("case-%03d", index),
				Data: out,
			})
		}
	}

	archive := txtar.Format(&txtar.Archive{
		Comment: []byte(fmt.Sprintf(`Generated by %s`, t.Name())),
		Files:   files,
	})

	require.NoError(t, os.WriteFile("testdata/template-cases-generated.txtar", archive, 0o644))
}

func makeSureTagIsNotEmptyString(values PartialValues, fuzzer *fuzz.Fuzzer) {
	if values.Image != nil && values.Image.Tag != nil && len(*values.Image.Tag) == 0 {
		t := values.Image.Tag
		for len(*t) == 0 {
			fuzzer.Fuzz(t)
		}
	}
}

func CalculateRoleRules(rules []rbacv1.PolicyRule) map[string]map[string]struct{} {
	flattened := map[string]map[string]struct{}{}
	for _, rule := range rules {
		for _, api := range rule.APIGroups {
			for _, res := range rule.Resources {
				key := fmt.Sprintf("%s#%s", api, res)

				if _, ok := flattened[key]; !ok {
					flattened[key] = map[string]struct{}{}
				}

				for _, verb := range rule.Verbs {
					flattened[key][verb] = struct{}{}
				}
			}
		}
	}
	return flattened
}

func ExtractRules(objs []kube.Object) (map[string]map[string]struct{}, map[string]map[string]struct{}) {
	var rules []rbacv1.PolicyRule
	var clusterRules []rbacv1.PolicyRule
	for _, o := range objs {
		switch obj := o.(type) {
		case *rbacv1.Role:
			rules = append(rules, obj.Rules...)
		case *rbacv1.ClusterRole:
			clusterRules = append(clusterRules, obj.Rules...)
		}
	}
	return CalculateRoleRules(clusterRules), CalculateRoleRules(rules)
}

func assertRulesSuperSet(t *testing.T, super, inner map[string]map[string]struct{}) {
	t.Helper()

	for resource, verbs := range inner {
		for verb := range verbs {
			if _, ok := super[resource][verb]; !ok {
				t.Errorf("super missing %q on %q", verb, resource)
			}
		}
	}
}
