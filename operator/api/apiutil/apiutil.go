// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package apiutil is a collection of types to aid in defining CRDs.
package apiutil

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// JSONBoolean is a workaround for accidental mistyping of the
// cloud_storage_enabled key and the attempted fix of v1alpha2.
// As the conversion was not correctly configured, v1alpha2 could contain
// cloud_storage_enabled fields with either a string or a boolean. This is not
// a type describable by the Kubernetes API.
// To work around any potential issue, we remove all Kubernetes API validation
// by extending the apiextensionsv1.JSON type. When we're marshaling this data
// back to JSON, this field will coalesce the value back into a boolean,
// falling back to false for any unknown values.
// +kubebuilder:object:generate=true
type JSONBoolean apiextensionsv1.JSON

func (in *JSONBoolean) MarshalJSON() ([]byte, error) {
	// Marshal any "known" true value to the boolean true and everything else
	// to false.
	switch string(in.Raw) {
	case `true`, `"true"`:
		return []byte(`true`), nil
	default:
		return []byte(`false`), nil
	}
}

func (in *JSONBoolean) UnmarshalJSON(data []byte) error {
	in.Raw = data
	return nil
}
