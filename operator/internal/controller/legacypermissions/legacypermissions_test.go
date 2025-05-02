package legacypermissions

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	operator "github.com/redpanda-data/redpanda-operator/operator/chart"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// TestLegacyPermissions asserts that the kubebuilder annotations in this
// package are sufficiently scoped such that older versions of the redpanda
// helm chart can be successfully deployed by the operator without any RBAC
// issues.
// As of 5.9.22 and v5.10.2 the chart and operator had their RBAC configs
// itemized and synchronized which reduced them to the minimal required set. As
// the operator's permissions must be a super set of anything it deploys, we
// created some issues by reducing the scope of both, hence the existence of
// this test.
func TestLegacyPermissions(t *testing.T) {
	ctx := context.Background()

	client, err := helm.New(helm.Options{})
	require.NoError(t, err)

	require.NoError(t, client.RepoAdd(ctx, "redpanda", "https://charts.redpanda.com"))

	cases := []struct {
		Name           string
		RedpandaValues redpanda.PartialValues
		OperatorValues operator.PartialValues
	}{
		{
			Name:           "defaults",
			RedpandaValues: redpanda.PartialValues{},
			OperatorValues: operator.PartialValues{},
		},
		{
			Name: "additional-controllers",
			RedpandaValues: redpanda.PartialValues{
				RBAC: &redpanda.PartialRBAC{
					Enabled: ptr.To(true), // Enable as many permissions as possible.
				},
			},
			OperatorValues: operator.PartialValues{
				RBAC: &operator.PartialRBAC{
					CreateAdditionalControllerCRs: ptr.To(true),
				},
			},
		},
	}

	// List of versions prior to the RBAC itemization change.
	for _, version := range []string{
		"v5.10.1",
		"v5.9.21",
	} {
		for _, tc := range cases {
			t.Run(tc.Name+"-"+version, func(t *testing.T) {
				out, err := client.Template(ctx, "redpanda/redpanda", helm.TemplateOptions{
					Name:    "redpanda",
					Version: version,
					Values:  tc.RedpandaValues,
				})
				require.NoError(t, err)

				objs, err := kube.DecodeYAML(out, redpanda.Scheme)
				require.NoError(t, err)

				curObjs, err := operator.Chart.Render(nil, helmette.Release{}, tc.OperatorValues)
				require.NoError(t, err)

				oldClusterRules, oldNamespaceRules := ExtractRules(objs)
				curClusterRoles, curNamespaceRules := ExtractRules(curObjs)

				// Assert that the current rules are a super set of the old ones.
				assertRulesSuperSet(t, curClusterRoles, oldClusterRules)
				assertRulesSuperSet(t, curNamespaceRules, oldNamespaceRules)
			})
		}
	}
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
