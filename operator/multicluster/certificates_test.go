// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestPoolCertificates(t *testing.T) {
	certs := poolCertificates(poolSpec(true), "pool")
	require.Equal(t, []string{"default", "external"}, slices.Sorted(maps.Keys(certs)))
	// Only admin requires mTLS.
	require.NotNil(t, certs["default"].Client)
	require.Nil(t, certs["external"].Client)

	require.Empty(t, poolCertificates(poolSpec(false), "pool"))

	// A listener without TLS keeps its certificate, because the spec contains
	// its name.
	quiet := poolSpec(true)
	quiet.Listeners.Kafka.TLS.Enabled = ptr.To(false)
	require.Contains(t, poolCertificates(quiet, "pool"), "default")
	require.Nil(t, resolveForSpec(quiet, false).Kafka().Reserved.TLS)

	require.Empty(t, poolCertificates(nil, "pool"))
	require.Empty(t, poolCertificates(&redpandav1alpha2.BrokerPoolSpec{}, "pool"))
}
