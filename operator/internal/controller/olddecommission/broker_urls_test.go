// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package olddecommission

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestCreateBrokerURLs(t *testing.T) {
	values := func(fullnameOverride string) map[string]interface{} {
		return map[string]interface{}{
			"fullnameOverride": fullnameOverride,
			"clusterDomain":    "cluster.local",
			"listeners": map[string]interface{}{
				"admin": map[string]interface{}{
					"port": float64(9644),
				},
			},
		}
	}

	t.Run("fullnameOverride differs from release: pod host uses the fullname", func(t *testing.T) {
		// Regression for the fullnameOverride decommission bug: with release
		// "redpanda-helm" and fullnameOverride "redpanda" the broker Pod is
		// redpanda-N (StatefulSet/fullname), not redpanda-helm-N.
		urls, err := createBrokerURLs("redpanda-helm", "dev-redpanda", 3, nil, values("redpanda"))
		require.NoError(t, err)
		require.Equal(t, []string{
			"redpanda-0.redpanda.dev-redpanda.svc.cluster.local:9644",
			"redpanda-1.redpanda.dev-redpanda.svc.cluster.local:9644",
			"redpanda-2.redpanda.dev-redpanda.svc.cluster.local:9644",
		}, urls)
	})

	t.Run("single ordinal uses the fullname", func(t *testing.T) {
		urls, err := createBrokerURLs("redpanda-helm", "dev-redpanda", 3, ptr.To(2), values("redpanda"))
		require.NoError(t, err)
		require.Equal(t, []string{"redpanda-2.redpanda.dev-redpanda.svc.cluster.local:9644"}, urls)
	})

	t.Run("no fullnameOverride: pod host falls back to the release name", func(t *testing.T) {
		urls, err := createBrokerURLs("redpanda", "ns", 1, nil, values(""))
		require.NoError(t, err)
		require.Equal(t, []string{"redpanda-0.redpanda.ns.svc.cluster.local:9644"}, urls)
	})
}
