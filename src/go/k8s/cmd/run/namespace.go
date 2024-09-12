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
	redpandacontrollers "github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/controller/redpanda"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *runner) addNamespaceReconcilers(mgr ctrl.Manager) *SetupError {
	ctrl.Log.Info("running as a namespace controller", "mode", NamespaceControllerMode, "namespace", r.namespace)
	if runThisController(NodeController, r.additionalControllers) {
		if err := (&redpandacontrollers.RedpandaNodePVCReconciler{
			Client:       mgr.GetClient(),
			OperatorMode: r.operatorMode,
		}).SetupWithManager(mgr); err != nil {
			return setupError(err, "unable to create controller", "controller", "RedpandaNodePVCReconciler")
		}
	}

	if runThisController(DecommissionController, r.additionalControllers) {
		if err := (&redpandacontrollers.DecommissionReconciler{
			Client:                   mgr.GetClient(),
			OperatorMode:             r.operatorMode,
			DecommissionWaitInterval: r.decommissionWaitInterval,
		}).SetupWithManager(mgr); err != nil {
			return setupError(err, "unable to create controller", "controller", "DecommissionReconciler")
		}
	}

	return nil
}
