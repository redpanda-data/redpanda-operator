// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

func (r *runner) addHealthChecks(mgr ctrl.Manager) *SetupError {
	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		return setupError(err, "Unable to set up health check")
	}

	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		return setupError(err, "Unable to set up ready check")
	}

	return nil
}
