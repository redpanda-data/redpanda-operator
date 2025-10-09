// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"

	_ "github.com/redpanda-data/console/backend/pkg/config" // NB: This import allows us to lock the version of the partial config being generated. The package is otherwise unused.
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

func StaticConfigurationSourceToPartialRenderValues(src *ir.StaticConfigurationSource) PartialRenderValues {
	mapper := &configMapper{
		Volumes: &volumes{
			Name:       "redpanda-certificates",
			Dir:        "/etc/tls/certs",
			Secrets:    map[string]map[string]bool{},
			ConfigMaps: map[string]map[string]bool{},
		},
	}

	// NB: This call mutates mapper to populate Volumes and VolumeMounts.
	cfg := mapper.toConfig(src)

	return PartialRenderValues{
		Config:            helmette.UnmarshalInto[map[string]any](cfg),
		ExtraEnv:          mapper.Env,
		ExtraVolumes:      mapper.Volumes.Volumes(),
		ExtraVolumeMounts: mapper.Volumes.VolumeMounts(),
	}
}

// configMapper is a helper struct that generates a [PartialConfig] and any
// volume mounts or environment variables it may need.
//
// NB: All conversion is written in a strange way to appease limitations of gotohelm.
//
// Most important is marshalling limitations. If an omitempty is explicitly
// assigned nil, gotohelm WILL output it but go will not. For this reason, all
// assignments are wrapped in nil checks. If one is made incorrectly a helm <->
// go equivalence test will catch it.
type configMapper struct {
	Volumes *volumes
	Env     []corev1.EnvVar
}

func (m *configMapper) toConfig(src *ir.StaticConfigurationSource) *PartialConfig {
	cfg := &PartialConfig{
		Redpanda: &PartialRedpanda{},
	}

	if kafka := m.configureKafka(src.Kafka); kafka != nil {
		cfg.Kafka = kafka
	}

	if admin := m.configureAdmin(src.Admin); admin != nil {
		cfg.Redpanda.AdminAPI = admin
	}

	if schema := m.configureSchemaRegistry(src.SchemaRegistry); schema != nil {
		cfg.SchemaRegistry = schema
	}

	return cfg
}

func (m *configMapper) configureAdmin(admin *ir.AdminAPISpec) *PartialRedpandaAdminAPI {
	if admin == nil {
		return nil
	}

	cfg := &PartialRedpandaAdminAPI{
		Enabled: ptr.To(true),
		URLs:    admin.URLs,
	}

	if tls := m.configureTLS(admin.TLS); tls != nil {
		cfg.TLS = tls
	}

	if admin.Auth != nil {
		cfg.Authentication = &PartialHTTPAuthentication{
			BasicAuth: &PartialHTTPBasicAuth{
				Username: &admin.Auth.Username,
			},
		}

		m.addEnv("REDPANDA_ADMINAPI_AUTHENTICATION_BASIC_PASSWORD", admin.Auth.Password)
	}

	return cfg
}

func (m *configMapper) configureKafka(kafka *ir.KafkaAPISpec) *PartialKafka {
	if kafka == nil {
		return nil
	}

	cfg := &PartialKafka{
		Brokers: kafka.Brokers,
	}

	if tls := m.configureTLS(kafka.TLS); tls != nil {
		cfg.TLS = tls
	}

	if kafka.SASL != nil {
		cfg.SASL = &PartialKafkaSASL{
			Enabled:   ptr.To(true),
			Username:  &kafka.SASL.Username,
			Mechanism: ptr.To(string(kafka.SASL.Mechanism)),
			// TODO all the other ones......
		}
		m.addEnv("KAFKA_SASL_PASSWORD", kafka.SASL.Password)
	}

	return cfg
}

func (m *configMapper) configureSchemaRegistry(schema *ir.SchemaRegistrySpec) *PartialSchema {
	if schema == nil {
		return nil
	}

	cfg := &PartialSchema{
		Enabled: ptr.To(true),
		URLs:    schema.URLs,
		// BearerToken is set via env vars, if present.
		// Password is set via env vars, if present.
	}

	if tls := m.configureTLS(schema.TLS); tls != nil {
		cfg.TLS = tls
	}

	if schema.SASL != nil {
		cfg.Authentication = &PartialHTTPAuthentication{
			BasicAuth: &PartialHTTPBasicAuth{
				Username: &schema.SASL.Username,
			},
		}

		m.addEnv("SCHEMAREGISTRY_AUTHENTICATION_BASIC_PASSWORD", schema.SASL.Password)
		m.addEnv("SCHEMAREGISTRY_AUTHENTICATION_BEARERTOKEN", schema.SASL.AuthToken)
	}

	return cfg
}

func (m *configMapper) configureTLS(tls *ir.CommonTLS) *PartialTLS {
	if tls == nil {
		return nil
	}

	out := &PartialTLS{Enabled: ptr.To(true)}

	// Only ever insecureSkipTLSVerify to True.
	if tls.InsecureSkipTLSVerify {
		out.InsecureSkipTLSVerify = &tls.InsecureSkipTLSVerify
	}

	if ca := m.Volumes.MaybeAdd(tls.CaCert); ca != nil {
		out.CaFilepath = ca
	}

	if cert := m.Volumes.MaybeAddSecret(tls.Cert); cert != nil {
		out.CertFilepath = cert
	}

	if key := m.Volumes.MaybeAddSecret(tls.Key); key != nil {
		out.KeyFilepath = key
	}

	return out
}

func (m *configMapper) addEnv(name string, ref ir.SecretKeyRef) {
	if ref.Key == "" || ref.Name == "" {
		return
	}

	m.Env = append(m.Env, corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: ref.Name,
				},
				Key: ref.Key,
			},
		},
	})
}

type volumes struct {
	Name       string
	Dir        string
	Secrets    map[string]map[string]bool
	ConfigMaps map[string]map[string]bool
}

func (v *volumes) MaybeAdd(ref *ir.ObjectKeyRef) *string {
	if ref == nil {
		return nil
	}

	if cmr := ref.ConfigMapKeyRef; cmr != nil {
		return v.MaybeAddConfigMap(cmr)
	}

	if skr := ref.SecretKeyRef; skr != nil {
		return v.MaybeAddSecret(&ir.SecretKeyRef{
			Name: skr.Name,
			Key:  skr.Key,
		})
	}

	return nil
}

func (v *volumes) MaybeAddConfigMap(ref *corev1.ConfigMapKeySelector) *string {
	if ref == nil || (ref.Key == "" && ref.Name == "") {
		return nil
	}
	if _, ok := v.ConfigMaps[ref.Name]; !ok {
		v.ConfigMaps[ref.Name] = map[string]bool{}
	}
	v.ConfigMaps[ref.Name][ref.Key] = true
	return ptr.To(fmt.Sprintf("%s/configmaps/%s/%s", v.Dir, ref.Name, ref.Key))
}

func (v *volumes) MaybeAddSecret(ref *ir.SecretKeyRef) *string {
	if ref == nil || (ref.Key == "" && ref.Name == "") {
		return nil
	}
	if _, ok := v.Secrets[ref.Name]; !ok {
		v.Secrets[ref.Name] = map[string]bool{}
	}
	v.Secrets[ref.Name][ref.Key] = true
	return ptr.To(fmt.Sprintf("%s/secrets/%s/%s", v.Dir, ref.Name, ref.Key))
}

func (v *volumes) VolumeMounts() []corev1.VolumeMount {
	if len(v.Secrets) == 0 && len(v.ConfigMaps) == 0 {
		return nil
	}

	return []corev1.VolumeMount{{
		Name:      v.Name,
		MountPath: v.Dir,
	}}
}

func (v *volumes) Volumes() []corev1.Volume {
	if len(v.Secrets) == 0 && len(v.ConfigMaps) == 0 {
		return nil
	}

	var sources []corev1.VolumeProjection
	for _, secret := range helmette.SortedKeys(v.Secrets) {
		var items []corev1.KeyToPath
		for _, key := range helmette.SortedKeys(v.Secrets[secret]) {
			items = append(items, corev1.KeyToPath{
				Key:  key,
				Path: fmt.Sprintf("secrets/%s/%s", secret, key),
			})
		}

		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secret,
				},
				Items: items,
			},
		})
	}

	for _, configmap := range helmette.SortedKeys(v.ConfigMaps) {
		var items []corev1.KeyToPath
		for _, key := range helmette.SortedKeys(v.ConfigMaps[configmap]) {
			items = append(items, corev1.KeyToPath{
				Key:  key,
				Path: fmt.Sprintf("configmaps/%s/%s", configmap, key),
			})
		}

		sources = append(sources, corev1.VolumeProjection{
			ConfigMap: &corev1.ConfigMapProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configmap,
				},
				Items: items,
			},
		})
	}

	return []corev1.Volume{{
		Name: v.Name,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: sources,
			},
		},
	}}
}
