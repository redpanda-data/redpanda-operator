package redpanda_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

func TestOperatorRBACInSync(t *testing.T) {
	cases := []struct {
		Name          string
		Values        redpanda.PartialValues
		OperatorFiles []string
	}{
		{
			Name:   "defaults",
			Values: redpanda.PartialValues{},
			OperatorFiles: []string{
				"../../operator/config/rbac/itemized/leader-election.yaml",
				"../../operator/config/rbac/itemized/v2-manager.yaml",
			},
		},
		{
			Name: "broker-decommissioner",
			Values: redpanda.PartialValues{
				Statefulset: &redpanda.PartialStatefulset{
					SideCars: &redpanda.PartialSidecars{
						BrokerDecommissioner: &struct {
							Enabled                    *bool   "json:\"enabled,omitempty\""
							DecommissionAfter          *string "json:\"decommissionAfter,omitempty\""
							DecommissionRequeueTimeout *string "json:\"decommissionRequeueTimeout,omitempty\""
						}{
							Enabled: ptr.To(true),
						},
					},
				},
			},
			OperatorFiles: []string{
				"../../operator/config/rbac/itemized/decommission.yaml",
				"../../operator/config/rbac/itemized/leader-election.yaml",
				"../../operator/config/rbac/itemized/v2-manager.yaml",
			},
		},
		{
			Name: "debug-bundle",
			Values: redpanda.PartialValues{
				RBAC: &redpanda.PartialRBAC{
					Enabled: ptr.To(true),
				},
			},
			OperatorFiles: []string{
				"../../operator/config/rbac/itemized/rpk-debug-bundle.yaml",
				"../../operator/config/rbac/itemized/leader-election.yaml",
				"../../operator/config/rbac/itemized/v2-manager.yaml",
			},
		},
		{
			Name: "everything",
			Values: redpanda.PartialValues{
				RBAC: &redpanda.PartialRBAC{
					Enabled: ptr.To(true),
				},
				Statefulset: &redpanda.PartialStatefulset{
					SideCars: &redpanda.PartialSidecars{
						PVCUnbinder: &struct {
							Enabled     *bool   "json:\"enabled,omitempty\""
							UnbindAfter *string "json:\"unbindAfter,omitempty\""
						}{
							Enabled: ptr.To(true),
						},
						BrokerDecommissioner: &struct {
							Enabled                    *bool   "json:\"enabled,omitempty\""
							DecommissionAfter          *string "json:\"decommissionAfter,omitempty\""
							DecommissionRequeueTimeout *string "json:\"decommissionRequeueTimeout,omitempty\""
						}{
							Enabled: ptr.To(true),
						},
						ConfigWatcher: &struct {
							Enabled *bool "json:\"enabled,omitempty\""
						}{
							Enabled: ptr.To(true),
						},
						Controllers: &struct {
							Image              *redpanda.PartialImage "json:\"image,omitempty\""
							Enabled            *bool                  "json:\"enabled,omitempty\""
							CreateRBAC         *bool                  "json:\"createRBAC,omitempty\""
							HealthProbeAddress *string                "json:\"healthProbeAddress,omitempty\""
							MetricsAddress     *string                "json:\"metricsAddress,omitempty\""
							PprofAddress       *string                "json:\"pprofAddress,omitempty\""
							Run                []string               "json:\"run,omitempty\""
						}{
							Enabled:    ptr.To(true),
							CreateRBAC: ptr.To(true),
						},
					},
				},
			},
			OperatorFiles: []string{
				"../../operator/config/rbac/itemized/decommission.yaml",
				"../../operator/config/rbac/itemized/leader-election.yaml",
				"../../operator/config/rbac/itemized/node-watcher.yaml",
				"../../operator/config/rbac/itemized/old-decommission.yaml",
				"../../operator/config/rbac/itemized/rpk-debug-bundle.yaml",
				"../../operator/config/rbac/itemized/v2-manager.yaml",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var operatorObjs []kube.Object
			for _, path := range tc.OperatorFiles {
				data, err := os.ReadFile(path)
				require.NoError(t, err)

				objs, err := kube.DecodeYAML(data, redpanda.Scheme)
				require.NoError(t, err)

				operatorObjs = append(operatorObjs, objs...)
			}

			objs, err := redpanda.Chart.Render(nil, helmette.Release{}, tc.Values)
			require.NoError(t, err)

			redpandaClusterRules, redpandaNamespaceRules := ExtractRules(objs)
			operatorClusterRules, operatorNamespaceRules := ExtractRules(operatorObjs)

			for _, args := range []struct {
				scope    string
				chart    map[string]map[string]struct{}
				operator map[string]map[string]struct{}
			}{
				{
					scope:    "Cluster",
					chart:    redpandaClusterRules,
					operator: operatorClusterRules,
				},
				{
					scope:    "Namespace",
					chart:    redpandaNamespaceRules,
					operator: operatorNamespaceRules,
				},
			} {
				// Assert that the operator's RBAC rules are a superset of redpanda's
				for resource, verbs := range args.chart {
					operatorVerbs := args.operator[resource]
					for verb := range verbs {
						if _, ok := operatorVerbs[verb]; !ok {
							t.Errorf("[%s Scope] operator missing %q on %q", args.scope, verb, resource)
						}
					}
				}
			}
		})
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
