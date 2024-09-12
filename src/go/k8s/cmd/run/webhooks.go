// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package run

import ctrl "sigs.k8s.io/controller-runtime"

func (r *runner) maybeAddWebhooks(mgr ctrl.Manager) *SetupError {
	if r.webhookEnabled {
		hookServer := mgr.GetWebhookServer()
		if err := mgr.AddReadyzCheck("webhook", hookServer.StartedChecker()); err != nil {
			return setupError(err, "unable to create ready check")
		}

		if err := mgr.AddHealthzCheck("webhook", hookServer.StartedChecker()); err != nil {
			return setupError(err, "unable to create health check")
		}
	}

	return nil
}
