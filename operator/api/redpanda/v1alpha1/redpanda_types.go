// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import (
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

var RedpandaChartRepository = "https://charts.redpanda.com/"

// Redpanda defines the CRD for Redpanda clusters.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=redpandas
// +kubebuilder:resource:shortName=rp
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
type Redpanda redpandav1alpha2.Redpanda

// RedpandaList contains a list of Redpanda objects.
// +kubebuilder:object:root=true
type RedpandaList redpandav1alpha2.RedpandaList

func init() {
	SchemeBuilder.Register(&Redpanda{}, &RedpandaList{})
}
