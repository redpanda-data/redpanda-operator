// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"testing"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/redpanda-data/redpanda-operator/harpoon/internal/testing"
)

func TestBuildTagExpression(t *testing.T) {
	for name, tt := range map[string]struct {
		groups    map[string]string
		requested []string
		provider  string
		want      string
	}{
		"no-groups-registered": {
			provider: "k3d",
			want:     "~@skip:k3d",
		},
		"single-group-default-only": {
			groups:   map[string]string{"multicluster": "multicluster"},
			provider: "k3d",
			want:     "~@skip:k3d && ~@multicluster",
		},
		"single-group-requested": {
			groups:    map[string]string{"multicluster": "multicluster"},
			requested: []string{"multicluster"},
			provider:  "k3d",
			want:      "~@skip:k3d && @multicluster",
		},
		"two-groups-default-only": {
			groups:   map[string]string{"multicluster": "multicluster", "dev-env": "dev-env"},
			provider: "k3d",
			want:     "~@skip:k3d && ~@dev-env && ~@multicluster",
		},
		"two-groups-one-requested": {
			groups:    map[string]string{"multicluster": "multicluster", "dev-env": "dev-env"},
			requested: []string{"multicluster"},
			provider:  "k3d",
			want:      "~@skip:k3d && @multicluster",
		},
		"two-groups-default-plus-one": {
			groups:    map[string]string{"multicluster": "multicluster", "dev-env": "dev-env"},
			requested: []string{"default", "multicluster"},
			provider:  "k3d",
			want:      "~@skip:k3d && @multicluster,~@dev-env",
		},
		"two-groups-all-requested": {
			groups:    map[string]string{"multicluster": "multicluster", "dev-env": "dev-env"},
			requested: []string{"default", "multicluster", "dev-env"},
			provider:  "k3d",
			want:      "~@skip:k3d",
		},
		"two-groups-both-non-default": {
			groups:    map[string]string{"multicluster": "multicluster", "dev-env": "dev-env"},
			requested: []string{"multicluster", "dev-env"},
			provider:  "k3d",
			want:      "~@skip:k3d && @dev-env,@multicluster",
		},
		"three-groups-default-plus-one": {
			groups:    map[string]string{"a": "a", "b": "b", "c": "c"},
			requested: []string{"default", "a"},
			provider:  "k3d",
			want:      "~@skip:k3d && @a,~@b && @a,~@c",
		},
		"unknown-group-requested": {
			groups:    map[string]string{"multicluster": "multicluster"},
			requested: []string{"nope"},
			provider:  "k3d",
			want:      "~@skip:k3d && @__no_matching_group__",
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := &SuiteBuilder{
				testingOpts:      &internaltesting.TestingOptions{Provider: tt.provider},
				registeredGroups: tt.groups,
				requestedGroups:  tt.requested,
			}
			require.Equal(t, tt.want, b.buildTagExpression())
		})
	}
}
