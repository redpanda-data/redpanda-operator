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

	// goverter:context namespace
	ConvertKafkaAPISpecToIR func(namespace string, src *KafkaAPISpec) *ir.KafkaAPISpec

	// goverter:map SASL Auth
	// goverter:context namespace
	// AdminAPI auth isn't technically SASL; it's been renamed.
	ConvertAdminAPISpecToIR func(namespace string, src *AdminAPISpec) *ir.AdminAPISpec

	// goverter:context namespace
	ConvertSchemaRegistrySpecToIR func(namespace string, src *SchemaRegistrySpec) *ir.SchemaRegistrySpec

	// Private conversions for tuning / customizing conversions.
	// Naming conversion: `autoconv_<Type>_To_<pkg>_<Type>`

	// goverter:ignore Create
	// Ability to disable creation of Deployment is not exposed through the Console CRD.
	autoconv_DeploymentConfig_console_PartialDeploymentConfig func(*DeploymentConfig) *console.PartialDeploymentConfig

	// goverter:ignore Create
	// Ability to disable creation of service account is not exposed through the Console CRD.
	autoconv_ServiceAccountConfig_To_console_PartialServiceAccountConfig func(*ServiceAccountConfig) *console.PartialServiceAccountConfig

	// goverter:map Namespace | getNamespace
	// goverter:context namespace
	autoconv_ValueSource_To_ir_ValueSource func(_ *ValueSource, namespace string) *ir.ValueSource
)

// getNamespace returns the namespace context argument to set fields on nested
// fields.
// goverter:context namespace
func getNamespace(namespace string) string {
	return namespace
}

//goverter:context namespace
func conv_CommonTLS_To_ir_CommonTLS(tls *CommonTLS, namespace string) *ir.CommonTLS {
	if tls == nil {
		return nil
	}

	commonTLS := &ir.CommonTLS{
		CaCert: autoconv_ValueSource_To_ir_ValueSource(tls.CaCert, namespace),
		Cert:   autoconv_ValueSource_To_ir_ValueSource(tls.Cert, namespace),
		Key:    autoconv_ValueSource_To_ir_ValueSource(tls.Key, namespace),
	}

	if tls.DeprecatedCaCert != nil {
		commonTLS.CaCert = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(tls.DeprecatedCaCert, namespace)
	}
	if tls.DeprecatedCert != nil {
		commonTLS.Cert = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(tls.DeprecatedCert, namespace)
	}
	if tls.DeprecatedKey != nil {
		commonTLS.Key = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(tls.DeprecatedKey, namespace)
	}

	commonTLS.InsecureSkipTLSVerify = tls.InsecureSkipTLSVerify

	return commonTLS
}

//goverter:context namespace
func conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(skr *SecretKeyRef, namespace string) *ir.ValueSource {
	if skr == nil {
		return nil
	}
	// Internal type supports ConfigMaps and Secrets. Public API only supports
	// Secrets.
	return &ir.ValueSource{
		Namespace: namespace,
		SecretKeyRef: &corev1.SecretKeySelector{
			Key: skr.Key,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: skr.Name,
			},
		},
	}
}

//goverter:context namespace
func conv_SecretKeyRef_To_ir_ValueSourcePtr(skr SecretKeyRef, namespace string) *ir.ValueSource {
	if skr.Name == "" {
		return nil
	}

	return conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(&skr, namespace)
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
