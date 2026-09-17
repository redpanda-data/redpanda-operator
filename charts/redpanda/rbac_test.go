// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"embed"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// generatedRules is controller-gen's output, one object per file named
// "<catalog key>.<kind>.yaml". The catalog in rbac.go is hand-written as
// gotohelm doesn't (easily) support go:embed in packages outside of the root
// chart.
//
//go:embed testdata/rbac/*.Role.yaml
//go:embed testdata/rbac/*.ClusterRole.yaml
var generatedRules embed.FS

// TestRoleRulesMatchGenerated asserts both catalogs equal the generated
// fixtures. The comparison is whole-map, so neither a new bundle nor a new
// catalog entry can land without the other.
func TestRoleRulesMatchGenerated(t *testing.T) {
	require.Equal(t, fixtureRules(t, "*.Role.yaml"), roleRules())
	require.Equal(t, fixtureRules(t, "*.ClusterRole.yaml"), clusterRoleRules())
}

// fixtureRules reads the fixtures matching pattern, keyed by the catalog key
// their filename leads with.
func fixtureRules(t *testing.T, pattern string) map[string][]rbacv1.PolicyRule {
	t.Helper()

	matches, err := fs.Glob(generatedRules, path.Join("testdata", "rbac", pattern))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "no fixtures matched %q", pattern)

	suffix := strings.TrimPrefix(pattern, "*")

	rules := map[string][]rbacv1.PolicyRule{}
	for _, match := range matches {
		contents, err := generatedRules.ReadFile(match)
		require.NoError(t, err)

		var role rbacv1.Role
		require.NoError(t, yaml.Unmarshal(contents, &role))

		rules[strings.TrimSuffix(path.Base(match), suffix)] = role.Rules
	}

	return rules
}

// TestToggleFieldsCoverCatalog asserts every catalog entry has a toggle field
// and vice versa. Without it, adding a rule set and a field but forgetting to
// pair them renders nothing, silently.
func TestToggleFieldsCoverCatalog(t *testing.T) {
	var rs RoleSet

	require.Equal(t, slices.Sorted(maps.Keys(roleRules())), slices.Sorted(maps.Keys(rs.roleToggles())))
	require.Equal(t, slices.Sorted(maps.Keys(clusterRoleRules())), slices.Sorted(maps.Keys(rs.clusterRoleToggles())))
}

// TestRoleSetRender pins naming, labels, annotations, binding derivation, and
// emission order in one artifact. Order is load-bearing: it's what both
// callers' goldens are recorded against.
func TestRoleSetRender(t *testing.T) {
	rs := RoleSet{
		Prefix:         "release",
		Namespace:      "ns",
		Labels:         map[string]string{"app.kubernetes.io/name": "redpanda"},
		Annotations:    map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::1:role/rp"},
		ServiceAccount: "release-sa",
		Roles: Roles{
			RPKDebugBundle: true,
			Sidecar:        true,
		},
		ClusterRoles: ClusterRoles{
			MetricsReader: true,
			RackAwareness: true,
		},
	}

	rendered, err := yaml.Marshal(rs.Render())
	require.NoError(t, err)

	testutil.AssertGolden(t, testutil.YAML, "./testdata/roleset.golden", rendered)
}
