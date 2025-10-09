// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package feature

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlags(t *testing.T) {
	t.Parallel()

	for _, bundle := range bundles {
		flags := make(map[string]struct{}, len(bundle))
		for _, flag := range bundle {
			// All defaults should successfully parse
			_, err := flag.Parse(flag.Default)
			assert.NoErrorf(t, err, "%q's default failed to parse: %q", flag.Key, flag.Default)

			// All flag names must be unique
			assert.NotContains(t, flag.Key, "%q is a non-unique flag", flag.Key)
			flags[flag.Key] = struct{}{}

		}
	}
}

func TestSetDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		In      Annotations
		Out     Annotations
		Changed bool
	}{
		{
			In: Annotations{},
			Out: Annotations{
				Annotations: map[string]string{
					"cluster.redpanda.com/managed":                           "true",
					"operator.redpanda.com/config-sync-mode":                 "additive",
					"operator.redpanda.com/restart-cluster-on-config-change": "false",
				},
			},
			Changed: true,
		},
		{
			In: Annotations{
				Annotations: map[string]string{
					"cluster.redpanda.com/managed":                           "true",
					"operator.redpanda.com/config-sync-mode":                 "additive",
					"operator.redpanda.com/restart-cluster-on-config-change": "false",
				},
			},
			Out: Annotations{
				Annotations: map[string]string{
					"cluster.redpanda.com/managed":                           "true",
					"operator.redpanda.com/config-sync-mode":                 "additive",
					"operator.redpanda.com/restart-cluster-on-config-change": "false",
				},
			},
			Changed: false,
		},
		{
			In: Annotations{
				Annotations: map[string]string{
					"operator.redpanda.com/config-sync-mode":                 "",
					"cluster.redpanda.com/managed":                           "",
					"operator.redpanda.com/restart-cluster-on-config-change": "",
				},
			},
			Out: Annotations{
				Annotations: map[string]string{
					"cluster.redpanda.com/managed":                           "",
					"operator.redpanda.com/config-sync-mode":                 "",
					"operator.redpanda.com/restart-cluster-on-config-change": "",
				},
			},
			Changed: false,
		},
	}

	for _, tc := range cases {
		SetDefaults(t.Context(), V2Flags, &tc.In)
		assert.Equal(t, tc.Out, tc.In)
	}
}

type Annotations struct{ Annotations map[string]string }

func (a *Annotations) GetAnnotations() map[string]string {
	return a.Annotations
}

func (a *Annotations) SetAnnotations(annos map[string]string) {
	a.Annotations = annos
}
