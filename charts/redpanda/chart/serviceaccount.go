// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_serviceaccount.go.tpl
package chart

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// Create the name of the service account to use
func ServiceAccountName(state *RenderState) string {
	serviceAccount := state.Values.ServiceAccount

	if serviceAccount.Create && serviceAccount.Name != "" {
		return serviceAccount.Name
	} else if serviceAccount.Create {
		return Fullname(state)
	} else if serviceAccount.Name != "" {
		return serviceAccount.Name
	}

	return "default"
}

func ServiceAccount(state *RenderState) *corev1.ServiceAccount {
	if !state.Values.ServiceAccount.Create {
		return nil
	}

	return redpanda.ServiceAccount(
		ServiceAccountName(state),
		state.Release.Namespace,
		FullLabels(state),
		helmette.Merge(state.Values.ServiceAccount.Annotations, FullAnnotations(state)),
	)
}
