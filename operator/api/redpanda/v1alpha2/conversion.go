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

// Manually implemented conversion routines
// Naming conversion: `conv_<Type>_To_<pkg>_<Type>`

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
func conv_AdminSASL_To_ir_AdminAuth(sasl *AdminSASL, namespace string) *ir.AdminAuth {
	if sasl == nil {
		return nil
	}

	irAuth := &ir.AdminAuth{
		Username:  sasl.Username,
		Password:  autoconv_ValueSource_To_ir_ValueSource(sasl.Password, namespace),
		AuthToken: autoconv_ValueSource_To_ir_ValueSource(sasl.AuthToken, namespace),
	}

	if sasl.DeprecatedPassword != nil && sasl.DeprecatedPassword.Name != "" {
		irAuth.Password = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(sasl.DeprecatedPassword, namespace)
	}

	if sasl.DeprecatedAuthToken != nil && sasl.DeprecatedAuthToken.Name != "" {
		irAuth.AuthToken = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(sasl.DeprecatedAuthToken, namespace)
	}

	return irAuth
}

//goverter:context namespace
func conv_SchemaRegistrySASL_To_ir_SchemaRegistrySASL(sasl *SchemaRegistrySASL, namespace string) *ir.SchemaRegistrySASL {
	if sasl == nil {
		return nil
	}

	irSASL := &ir.SchemaRegistrySASL{
		Username:  sasl.Username,
		Password:  autoconv_ValueSource_To_ir_ValueSource(sasl.Password, namespace),
		AuthToken: autoconv_ValueSource_To_ir_ValueSource(sasl.AuthToken, namespace),
	}

	if sasl.DeprecatedPassword != nil && sasl.DeprecatedPassword.Name != "" {
		irSASL.Password = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(sasl.DeprecatedPassword, namespace)
	}

	if sasl.DeprecatedAuthToken != nil && sasl.DeprecatedAuthToken.Name != "" {
		irSASL.AuthToken = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(sasl.DeprecatedAuthToken, namespace)
	}

	return irSASL
}

//goverter:context namespace
func conv_KafkaSASL_To_ir_KafkaSASL(sasl *KafkaSASL, namespace string) *ir.KafkaSASL {
	if sasl == nil {
		return nil
	}

	irSASL := &ir.KafkaSASL{
		Username:     sasl.Username,
		Mechanism:    ir.SASLMechanism(sasl.Mechanism),
		Password:     autoconv_ValueSource_To_ir_ValueSource(sasl.Password, namespace),
		OAUth:        conv_KafkaSASLOauth_To_ir_KafkaSASLOauth(sasl.OAUth, namespace),
		GSSAPIConfig: conv_KafkaSASLGSSAPI_To_ir_KafkaSASLGSSAPI(sasl.GSSAPIConfig, namespace),
		AWSMskIam:    conv_KafkaSASLAWSMskIam_To_ir_KafkaSASLAWSMskIam(sasl.AWSMskIam, namespace),
	}

	if sasl.DeprecatedPassword != nil && sasl.DeprecatedPassword.Name != "" {
		irSASL.Password = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(sasl.DeprecatedPassword, namespace)
	}

	return irSASL
}

func conv_KafkaSASLAWSMskIam_To_ir_KafkaSASLAWSMskIam(iam *KafkaSASLAWSMskIam, namespace string) *ir.KafkaSASLAWSMskIam {
	if iam == nil {
		return nil
	}

	irIAM := &ir.KafkaSASLAWSMskIam{
		AccessKey:    iam.AccessKey,
		UserAgent:    iam.UserAgent,
		SecretKey:    autoconv_ValueSource_To_ir_ValueSource(iam.SecretKey, namespace),
		SessionToken: autoconv_ValueSource_To_ir_ValueSource(iam.SessionToken, namespace),
	}

	if iam.DeprecatedSecretKey != nil && iam.DeprecatedSecretKey.Name != "" {
		irIAM.SecretKey = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(iam.DeprecatedSecretKey, namespace)
	}

	if iam.DeprecatedSessionToken != nil && iam.DeprecatedSessionToken.Name != "" {
		irIAM.SessionToken = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(iam.DeprecatedSessionToken, namespace)
	}

	return irIAM
}

func conv_KafkaSASLOauth_To_ir_KafkaSASLOauth(oauth *KafkaSASLOAuthBearer, namespace string) *ir.KafkaSASLOAuthBearer {
	if oauth == nil {
		return nil
	}

	irOauth := &ir.KafkaSASLOAuthBearer{
		Token: autoconv_ValueSource_To_ir_ValueSource(oauth.Token, namespace),
	}

	if oauth.DeprecatedToken != nil && oauth.DeprecatedToken.Name != "" {
		irOauth.Token = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(oauth.DeprecatedToken, namespace)
	}

	return irOauth
}

func conv_KafkaSASLGSSAPI_To_ir_KafkaSASLGSSAPI(gssAPI *KafkaSASLGSSAPI, namespace string) *ir.KafkaSASLGSSAPI {
	if gssAPI == nil {
		return nil
	}

	irGSSAPI := &ir.KafkaSASLGSSAPI{
		AuthType:           gssAPI.AuthType,
		KeyTabPath:         gssAPI.KeyTabPath,
		KerberosConfigPath: gssAPI.KerberosConfigPath,
		ServiceName:        gssAPI.ServiceName,
		Username:           gssAPI.Username,
		Password:           autoconv_ValueSource_To_ir_ValueSource(gssAPI.Password, namespace),
		Realm:              gssAPI.Realm,
		EnableFast:         gssAPI.EnableFast,
	}

	if gssAPI.DeprecatedPassword != nil && gssAPI.DeprecatedPassword.Name != "" {
		irGSSAPI.Password = conv_SecretKeyRefPtr_To_ir_ValueSourcePtr(gssAPI.DeprecatedPassword, namespace)
	}

	return irGSSAPI
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
