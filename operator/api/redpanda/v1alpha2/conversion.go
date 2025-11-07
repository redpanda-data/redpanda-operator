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
	appsv1 "k8s.io/api/apps/v1"
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
	// Naming convention: `autoconv_<Type>_To_<pkg>_<Type>`

	// goverter:map . LicenseSecretRef | convertConsoleLicenseSecretRef
	// goverter:ignore Config
	// goverter:ignore Warnings
	// goverter:ignore ExtraContainerPorts
	autoconv_RedpandaConsole_To_ConsoleValues func(*RedpandaConsole) (*ConsoleValues, error)

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

// convertConsoleLicenseSecretRef extracts either the LicenseSecretRef or
// Enterprise.LicenseSecret from a [RedpandaConsole] into a
// [corev1.SecretKeySelector].
func convertConsoleLicenseSecretRef(src *RedpandaConsole) (*corev1.SecretKeySelector, error) {
	// If LicenseSecreRef is set, accept that.
	if src.LicenseSecretRef != nil {
		return src.LicenseSecretRef, nil
	}

	// Short circuit if Enterprise isn't specified.
	if src.Enterprise == nil || len(src.Enterprise.Raw) != 0 {
		return nil, nil
	}

	// Otherwise attempt to extract a secret reference from the Enterprise block.
	type ConsoleEnterprise struct {
		LicenseSecret *corev1.SecretKeySelector
	}

	enterprise, err := convertRuntimeRawExtension[ConsoleEnterprise](src.Enterprise)
	if err != nil {
		return nil, err
	}

	return enterprise.LicenseSecret, nil
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

var (
	conv_corev1_Volume_To_corev1_Volume                             = convertDeepCopier[corev1.Volume]
	conv_corev1_EnvVar_To_corev1EnvVar                              = convertDeepCopier[corev1.EnvVar]
	conv_corev1_ResourceRequirements_To_corev1_ResourceRequirements = convertDeepCopier[corev1.ResourceRequirements]

	//  RawExtension -> Custom type (RedpandaConsole -> Console)

	conv_runtime_RawExtension_To_mapstringany         = convertRuntimeRawExtension[map[string]any]
	conv_runtime_RawExtension_To_mapstringstring      = convertRuntimeRawExtension[map[string]string]
	conv_runtime_RawExtension_To_Image                = convertRuntimeRawExtension[*Image]
	conv_runtime_RawExtension_To_ServiceAccountConfig = convertRuntimeRawExtension[*ServiceAccountConfig]
	conv_runtime_RawExtension_To_Service              = convertRuntimeRawExtension[*ServiceConfig]
	conv_runtime_RawExtension_To_Ingress              = convertRuntimeRawExtension[*IngressConfig]
	conv_runtime_RawExtension_To_Autoscaling          = convertRuntimeRawExtension[*AutoScaling]
	conv_runtime_RawExtension_To_SecretMounts         = convertRuntimeRawExtension[SecretMount]
	conv_runtime_RawExtension_To_Secret               = convertRuntimeRawExtension[SecretConfig]
	conv_runtime_RawExtension_To_Deployment           = convertRuntimeRawExtension[*DeploymentConfig]

	// RawExtension -> built in types (RedpandaConsole -> Console)

	conv_runtime_RawExtension_To_corev1_Affinity                  = convertRuntimeRawExtension[*corev1.Affinity]
	conv_runtime_RawExtension_To_corev1_Container                 = convertRuntimeRawExtension[corev1.Container]
	conv_runtime_RawExtension_To_corev1_EnvFromSource             = convertRuntimeRawExtension[corev1.EnvFromSource]
	conv_runtime_RawExtension_To_corev1_EnvVar                    = convertRuntimeRawExtension[corev1.EnvVar]
	conv_runtime_RawExtension_To_corev1_LocalObjectReference      = convertRuntimeRawExtension[corev1.LocalObjectReference]
	conv_runtime_RawExtension_To_corev1_PodSecurityContext        = convertRuntimeRawExtension[*corev1.PodSecurityContext]
	conv_runtime_RawExtension_To_corev1_Resources                 = convertRuntimeRawExtension[*corev1.ResourceRequirements]
	conv_runtime_RawExtension_To_corev1_SecurityContext           = convertRuntimeRawExtension[*corev1.SecurityContext]
	conv_runtime_RawExtension_To_corev1_Strategy                  = convertRuntimeRawExtension[*appsv1.DeploymentStrategy]
	conv_runtime_RawExtension_To_corev1_Tolerations               = convertRuntimeRawExtension[corev1.Toleration]
	conv_runtime_RawExtension_To_corev1_TopologySpreadConstraints = convertRuntimeRawExtension[[]corev1.TopologySpreadConstraint]
	conv_runtime_RawExtension_To_corev1_Volume                    = convertRuntimeRawExtension[corev1.Volume]
	conv_runtime_RawExtension_To_corev1_VolumeMount               = convertRuntimeRawExtension[corev1.VolumeMount]

	// TODO THIS IS BAD AND BROKEN (Will write 0s for unspecified fields and generate invalid options).
	// ConsolePartialValues really needs to have ApplyConfigs for most k8s types.
	// Upgrade gen partial to pull an overridden type from a comment or field tag?
	conv_LivenessProbe_To_ProbeApplyConfiguration  = convertViaMarshaling[*LivenessProbe, *ProbeApplyConfiguration]
	conv_ProbeApplyConfiguration_To_corev1_Probe   = convertViaMarshaling[ProbeApplyConfiguration, corev1.Probe]
	conv_ReadinessProbe_To_ProbeApplyConfiguration = convertViaMarshaling[*ReadinessProbe, *ProbeApplyConfiguration]
)

type deepCopier[T any] interface {
	*T
	DeepCopy() *T
}

func convertDeepCopier[T any, P deepCopier[T]](in T) T {
	return *P(&in).DeepCopy()
}

func convertRuntimeRawExtension[T any](ext *runtime.RawExtension) (T, error) {
	if ext == nil {
		var zero T
		return zero, nil
	}

	var out T
	if err := json.Unmarshal(ext.Raw, &out); err != nil {
		var zero T
		return zero, errors.Wrapf(err, "unmarshalling %T into %T", ext, zero)
	}
	return out, nil
}

func convertViaMarshaling[From any, To any](src From) (To, error) {
	marshalled, err := json.Marshal(src)
	if err != nil {
		var zero To
		return zero, errors.Wrapf(err, "marshalling: %T", src)
	}

	var out To
	if err := json.Unmarshal(marshalled, &out); err != nil {
		var zero To
		return zero, errors.Wrapf(err, "unmarshalling %T into %T", src, zero)
	}

	return out, nil
}
