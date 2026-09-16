// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

//go:build !gotohelm

package chart

import corev1 "k8s.io/api/core/v1"

// SteerEndpoints hands svc's EndpointSlices to the operator's endpoint
// steering controller, which publishes them per port for the brokers of the
// named cluster. The selector goes, or the native EndpointSlice controller
// publishes every broker on every port alongside; the annotation names the
// cluster, and overrides any value a user set for the same key, since the
// controller relies on it.
//
// It is operator-only -- a Helm release has nothing to publish its endpoints
// -- so it lives outside the transpiled chart and is applied by the
// operator's renderers to whichever Services carry the cluster's Schema
// Registry listener.
func SteerEndpoints(svc *corev1.Service, cluster string) {
	svc.Spec.Selector = nil
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[EndpointSteeringAnnotation] = cluster
}
