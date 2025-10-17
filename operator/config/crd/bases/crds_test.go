// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package crds_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	crds "github.com/redpanda-data/redpanda-operator/operator/config/crd/bases"
)

func TestCRDS(t *testing.T) {
	names := map[string]struct{}{
		"clusters.redpanda.vectorized.io":           {},
		"consoles.cluster.redpanda.com":             {},
		"consoles.redpanda.vectorized.io":           {},
		"nodepools.cluster.redpanda.com":            {},
		"redpandas.cluster.redpanda.com":            {},
		"redpandaroles.cluster.redpanda.com":        {},
		"redpandarolebindings.cluster.redpanda.com": {},
		"schemas.cluster.redpanda.com":              {},
		"shadowlinks.cluster.redpanda.com":          {},
		"topics.cluster.redpanda.com":               {},
		"users.cluster.redpanda.com":                {},
	}

	foundNames := map[string]struct{}{}
	for _, crd := range crds.All() {
		foundNames[crd.Name] = struct{}{}
	}

	require.Equal(t, names, foundNames)

	require.Equal(t, "consoles.cluster.redpanda.com", crds.Console().Name)
	require.Equal(t, "nodepools.cluster.redpanda.com", crds.NodePool().Name)
	require.Equal(t, "redpandas.cluster.redpanda.com", crds.Redpanda().Name)
	require.Equal(t, "redpandaroles.cluster.redpanda.com", crds.Role().Name)
	require.Equal(t, "redpandarolebindings.cluster.redpanda.com", crds.RoleBinding().Name)
	require.Equal(t, "schemas.cluster.redpanda.com", crds.Schema().Name)
	require.Equal(t, "shadowlinks.cluster.redpanda.com", crds.ShadowLink().Name)
	require.Equal(t, "topics.cluster.redpanda.com", crds.Topic().Name)
	require.Equal(t, "users.cluster.redpanda.com", crds.User().Name)
}
