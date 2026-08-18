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
	"testing"

	"github.com/stretchr/testify/require"

	entcontroller "github.com/redpanda-data/redpanda-operator/enterprise/operator/controller"
)

// TestEnterpriseSharedConstantsDrift pins the controller constants this
// package shares with its enterprise counterpart. The enterprise module may
// not import this package (see enterprise/lint/boundary_test.go), so
// enterprise/operator/controller/shared.go carries deliberate duplicates; if
// the two sides drift, the stretch controllers and the OSS controllers start
// disagreeing about finalizer ownership and requeue cadence. This test lives
// in-package so it can read this package's unexported constants while
// comparing against the enterprise package's exported ones.
func TestEnterpriseSharedConstantsDrift(t *testing.T) {
	require.Equal(t, FinalizerKey, entcontroller.FinalizerKey,
		"FinalizerKey must be identical in both modules: the enterprise stretch controllers and the OSS controllers manage finalizers on resources interchangeably")
	require.Equal(t, requeueTimeout, entcontroller.RequeueTimeout,
		"requeueTimeout drifted from enterprise/operator/controller/shared.go")
	require.Equal(t, periodicRequeue, entcontroller.PeriodicRequeue,
		"periodicRequeue drifted from enterprise/operator/controller/shared.go")
	require.Equal(t, finalizerRequeueTimeout, entcontroller.FinalizerRequeueTimeout,
		"finalizerRequeueTimeout drifted from enterprise/operator/controller/shared.go")
	require.Equal(t, defaultReconcileTimeout, entcontroller.DefaultReconcileTimeout,
		"defaultReconcileTimeout drifted from enterprise/operator/controller/shared.go")
}
