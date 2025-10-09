// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

// goverter:variables
// goverter:output:format assign-variable
// goverter:output:file ./zz_generated.conversion.go
// goverter:enum no
// goverter:extend conv_.*
var (
	// Publicly accessible Conversion functions. Naming is a up to the implementer.

	// goverter:ignore ConfigMap
	// goverter:ignore InitContainers
	// goverter:ignore NameOverride
	// goverter:ignore FullnameOverride
	// Ability to disable ConfigMaps or specific templated initContainers removed.
	ConvertConsoleToConsolePartialRenderValues func(*ConsoleValues) (*console.PartialRenderValues, error)

	// goverter:context namespace
	ConvertStaticConfigToIR func(namespace string, src *StaticConfigurationSource) *ir.StaticConfigurationSource

	// Private conversions for tuning / customizing conversions.
	// Naming conversion: `autoconv_<Type>_To_<pkg>_<Type>`

	// goverter:ignore Create
	// Ability to disable creation of Deployment is not exposed through the Console CRD.
	autoconv_DeploymentConfig_console_PartialDeploymentConfig func(*DeploymentConfig) *console.PartialDeploymentConfig

	// goverter:ignore Create
	// Ability to disable creation of service account is not exposed through the Console CRD.
	autoconv_ServiceAccountConfig_To_console_PartialServiceAccountConfig func(*ServiceAccountConfig) *console.PartialServiceAccountConfig

	// goverter:map SASL Auth
	// goverter:context namespace
	// AdminAPI auth isn't technically SASL; it's been renamed.
	autoconv_AdminAPISpec_To_ir_AdminAPISpec func(_ *AdminAPISpec, namespace string) *ir.AdminAPISpec

	// goverter:map Namespace | getNamespace
	// goverter:context namespace
	autoconv_SecretKeyRef_To_ir_SecretKeyRef func(_ SecretKeyRef, namespace string) ir.SecretKeyRef

	// goverter:context namespace
	autoconv_CommonTLS_To_ir_CommonTLS func(_ *CommonTLS, namespace string) *ir.CommonTLS
)

// getNamespace returns the namespace context argument to set fields on nested
// fields.
// goverter:context namespace
func getNamespace(namespace string) string {
	return namespace
}

// Manually implemented conversion routines
// Naming conversion: `conv_<Type>_To_<pkg>_<Type>`

//goverter:context namespace
func conv_SecretKeyRef_To_ir_ObjectKeyRef(skr *SecretKeyRef, namespace string) *ir.ObjectKeyRef {
	if skr == nil {
		return nil
	}
	// Internal type supports ConfigMaps and Secrets. Public API only supports
	// Secrets.
	return &ir.ObjectKeyRef{
		Namespace: namespace,
		SecretKeyRef: &corev1.SecretKeySelector{
			Key: skr.Key,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: skr.Name,
			},
		},
	}
}

func conv_runtime_RawExtension_To_mapany(ext *runtime.RawExtension) (map[string]any, error) {
	if ext == nil {
		return nil, nil
	}

	var out map[string]any
	if err := json.Unmarshal(ext.Raw, &out); err != nil {
		return nil, errors.WithStack(err)
	}
	return out, nil
}

var (
	conv_corev1_Volume_To_corev1_Volume                             = convertDeepCopier[corev1.Volume]
	conv_corev1_EnvVar_To_corev1EnvVar                              = convertDeepCopier[corev1.EnvVar]
	conv_corev1_ResourceRequirements_To_corev1_ResourceRequirements = convertDeepCopier[corev1.ResourceRequirements]
)

type deepCopier[T any] interface {
	*T
	DeepCopy() *T
}

func convertDeepCopier[T any, P deepCopier[T]](in T) T {
	return *P(&in).DeepCopy()
}
