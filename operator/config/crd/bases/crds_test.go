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
		"clusters.redpanda.vectorized.io": {},
		"consoles.redpanda.vectorized.io": {},
		"redpandas.cluster.redpanda.com":  {},
		"schemas.cluster.redpanda.com":    {},
		"topics.cluster.redpanda.com":     {},
		"users.cluster.redpanda.com":      {},
	}

	foundNames := map[string]struct{}{}
	for _, crd := range crds.All() {
		foundNames[crd.Name] = struct{}{}
	}

	require.Equal(t, names, foundNames)

	require.Equal(t, "redpandas.cluster.redpanda.com", crds.Redpanda().Name)
	require.Equal(t, "topics.cluster.redpanda.com", crds.Topic().Name)
	require.Equal(t, "users.cluster.redpanda.com", crds.User().Name)
}
