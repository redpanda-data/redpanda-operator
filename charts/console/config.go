package console

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/ir"
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
	cfg := &PartialConfig{}

	if kafka := m.configureKafka(src); kafka != nil {
		cfg.Kafka = kafka
	}

	if admin := m.configureAdmin(src.Admin); admin != nil {
		cfg.Redpanda = &PartialRedpanda{
			AdminAPI: admin,
		}
	}

	// Always ensure Redpanda section exists
	if cfg.Redpanda == nil {
		cfg.Redpanda = &PartialRedpanda{}
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

	if admin.TLS != nil {
		cfg.TLS = &PartialRedpandaAdminAPITLS{Enabled: ptr.To(true)}

		// Only ever insecureSkipTLSVerify to True.
		if admin.TLS.InsecureSkipTLSVerify {
			cfg.TLS.InsecureSkipTLSVerify = &admin.TLS.InsecureSkipTLSVerify
		}

		if ca := m.Volumes.MaybeAdd(admin.TLS.CaCert); ca != nil {
			cfg.TLS.CaFilepath = ca
		}

		if cert := m.Volumes.MaybeAddSecret(admin.TLS.Cert); cert != nil {
			cfg.TLS.CertFilepath = cert
		}

		if key := m.Volumes.MaybeAddSecret(admin.TLS.Key); key != nil {
			cfg.TLS.KeyFilepath = key
		}
	}

	if admin.Auth != nil {
		cfg.Username = &admin.Auth.Username
		m.addEnv("REDPANDA_ADMINAPI_PASSWORD", admin.Auth.Password)
	}

	return cfg
}

func (m *configMapper) configureKafka(src *ir.StaticConfigurationSource) *PartialKafka {
	if src.Kafka == nil {
		return nil
	}

	cfg := &PartialKafka{
		Brokers: src.Kafka.Brokers,
		Schema:  m.configureSchemaRegistry(src.SchemaRegistry),
	}

	if src.Kafka.TLS != nil {
		cfg.TLS = &PartialKafkaTLS{Enabled: ptr.To(true)}

		// Only ever insecureSkipTLSVerify to True.
		if src.Kafka.TLS.InsecureSkipTLSVerify {
			cfg.TLS.InsecureSkipTLSVerify = &src.Kafka.TLS.InsecureSkipTLSVerify
		}

		if ca := m.Volumes.MaybeAdd(src.Kafka.TLS.CaCert); ca != nil {
			cfg.TLS.CaFilepath = ca
		}

		if cert := m.Volumes.MaybeAddSecret(src.Kafka.TLS.Cert); cert != nil {
			cfg.TLS.CertFilepath = cert
		}

		if key := m.Volumes.MaybeAddSecret(src.Kafka.TLS.Key); key != nil {
			cfg.TLS.KeyFilepath = key
		}
	}

	if src.Kafka.SASL != nil {
		cfg.SASL = &PartialKafkaSASL{
			Enabled:   ptr.To(true),
			Username:  &src.Kafka.SASL.Username,
			Mechanism: ptr.To(string(src.Kafka.SASL.Mechanism)),
			// TODO all the other ones......
		}
		m.addEnv("KAFKA_SASL_PASSWORD", src.Kafka.SASL.Password)
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
		// Password is et via env vars, if present.
	}

	if schema.TLS != nil {
		cfg.TLS = &PartialSchemaTLS{Enabled: ptr.To(true)}

		// Only ever insecureSkipTLSVerify to True.
		if schema.TLS.InsecureSkipTLSVerify {
			cfg.TLS.InsecureSkipTLSVerify = &schema.TLS.InsecureSkipTLSVerify
		}

		if ca := m.Volumes.MaybeAdd(schema.TLS.CaCert); ca != nil {
			cfg.TLS.CaFilepath = ca
		}

		if cert := m.Volumes.MaybeAddSecret(schema.TLS.Cert); cert != nil {
			cfg.TLS.CertFilepath = cert
		}

		if key := m.Volumes.MaybeAddSecret(schema.TLS.Key); key != nil {
			cfg.TLS.KeyFilepath = key
		}
	}

	if schema.SASL != nil {
		cfg.Username = &schema.SASL.Username
		m.addEnv("KAFKA_SCHEMA_PASSWORD", schema.SASL.Password)
		m.addEnv("KAFKA_SCHEMA_BEARERTOKEN", schema.SASL.AuthToken)
	}

	return cfg
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
