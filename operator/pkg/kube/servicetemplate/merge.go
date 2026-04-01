// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package servicetemplate provides strategic merge patch utilities for
// Kubernetes Service objects.
package servicetemplate

import (
	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// Overrides represents a set of Service overrides using Kubernetes
// apply-configuration types.
type Overrides struct {
	Labels      map[string]string                          `json:"labels,omitempty"`
	Annotations map[string]string                          `json:"annotations,omitempty"`
	Spec        *applycorev1.ServiceSpecApplyConfiguration `json:"spec,omitempty"`
}

// StrategicMergePatch applies Service overrides to an existing Service
// using a strategic-merge-patch-like approach. Ports are merged by name key
// rather than replaced wholesale.
func StrategicMergePatch(overrides Overrides, original corev1.Service) (corev1.Service, error) {
	var zero corev1.Service

	// Capture whether the override explicitly sets Selector before the JSON
	// round-trip, because omitempty drops empty maps during marshaling.
	selectorOverridden := overrides.Spec != nil && overrides.Spec.Selector != nil

	// Deep clone overrides via JSON round-trip.
	cloned := tplutil.FromJSON(tplutil.ToJSON(overrides))
	var err error
	overrides, err = tplutil.MergeTo[Overrides](cloned)
	if err != nil {
		return zero, err
	}

	overrideSpec := overrides.Spec
	if overrideSpec == nil {
		overrideSpec = &applycorev1.ServiceSpecApplyConfiguration{}
	}

	merged, err := tplutil.MergeTo[corev1.Service](
		applycorev1.ServiceApplyConfiguration{
			ObjectMetaApplyConfiguration: &applymetav1.ObjectMetaApplyConfiguration{
				Labels:      overrides.Labels,
				Annotations: overrides.Annotations,
			},
			Spec: overrideSpec,
		},
		original,
	)
	if err != nil {
		return zero, err
	}

	// Merge ports by name key.
	merged.Spec.Ports = tplutil.MergeSliceBy(
		original.Spec.Ports,
		overrideSpec.Ports,
		"name",
		mergeServicePort,
	)

	// Selector needs special handling: mergo won't overwrite a non-empty map
	// with an empty one, and omitempty drops empty maps during the JSON
	// round-trip. If the override explicitly set Selector (even to empty),
	// replace the original selector entirely.
	if selectorOverridden {
		merged.Spec.Selector = overrideSpec.Selector
	}

	return merged, nil
}

func mergeServicePort(original corev1.ServicePort, override applycorev1.ServicePortApplyConfiguration) corev1.ServicePort {
	result, _ := tplutil.MergeTo[corev1.ServicePort](override, original)
	return result
}
