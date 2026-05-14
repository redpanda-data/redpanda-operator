// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

// ConvertStretchClusterToStaticConfig derives a StaticConfigurationSource —
// brokers, admin URLs, schema-registry URLs, TLS, and SASL — from a
// StretchCluster spec. The output mirrors what the legacy chart path
// produces via RenderState.AsStaticConfigSource for a Redpanda CR, so
// downstream callers (Console controller, in particular) can consume both
// kinds of cluster references through the same StaticConfigurationSource
// type.
//
// All endpoint URLs point at the headless Service of the StretchCluster in
// the form `<name>.<namespace>.svc.<cluster-domain>:<port>`. This single
// endpoint reaches all brokers across all participating Kubernetes clusters
// because each cluster's local headless Service has EndpointSlices for
// every peer pod (under crossClusterMode=flat) or the equivalent for the
// other cross-cluster modes.
func ConvertStretchClusterToStaticConfig(sc *redpandav1alpha2.StretchCluster) *ir.StaticConfigurationSource {
	if sc == nil {
		return nil
	}

	spec := sc.Spec.DeepCopy()
	spec.MergeDefaults()

	host := strings.TrimSuffix(spec.InternalDomain(sc.Name, sc.Namespace), ".")

	cfg := &ir.StaticConfigurationSource{
		Kafka: stretchKafkaSpec(sc, spec, host),
		Admin: stretchAdminSpec(sc, spec, host),
	}
	if sr := stretchSchemaRegistrySpec(sc, spec, host); sr != nil {
		cfg.SchemaRegistry = sr
	}

	return cfg
}

func stretchKafkaSpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, host string) *ir.KafkaAPISpec {
	kafka := &ir.KafkaAPISpec{
		Brokers: []string{fmt.Sprintf("%s:%d", host, spec.KafkaPort())},
	}

	if listener := stretchListener(spec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener { return l.Kafka }); listener != nil && listener.IsTLSEnabled(spec.TLS) {
		kafka.TLS = stretchListenerTLS(sc, spec, listener)
	}

	if spec.Auth.IsSASLEnabled() {
		kafka.SASL = &ir.KafkaSASL{
			Username:  redpandav1alpha2.StretchClusterBootstrapUsername,
			Password:  stretchBootstrapPasswordSource(sc),
			Mechanism: ir.SASLMechanism(stretchSASLMechanism(spec)),
		}
	}

	return kafka
}

func stretchAdminSpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, host string) *ir.AdminAPISpec {
	scheme := "http"
	var tls *ir.CommonTLS
	if listener := stretchListener(spec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener { return l.Admin }); listener != nil && listener.IsTLSEnabled(spec.TLS) {
		scheme = "https"
		tls = stretchListenerTLS(sc, spec, listener)
	}

	admin := &ir.AdminAPISpec{
		URLs: []string{fmt.Sprintf("%s://%s:%d", scheme, host, spec.AdminPort())},
		TLS:  tls,
	}

	if spec.Auth.IsSASLEnabled() {
		admin.Auth = &ir.AdminAuth{
			Username: redpandav1alpha2.StretchClusterBootstrapUsername,
			Password: stretchBootstrapPasswordSource(sc),
		}
	}

	return admin
}

func stretchSchemaRegistrySpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, host string) *ir.SchemaRegistrySpec {
	listener := stretchListener(spec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener {
		return l.SchemaRegistry
	})

	scheme := "http"
	var tls *ir.CommonTLS
	if listener != nil && listener.IsTLSEnabled(spec.TLS) {
		scheme = "https"
		tls = stretchListenerTLS(sc, spec, listener)
	}

	sr := &ir.SchemaRegistrySpec{
		URLs: []string{fmt.Sprintf("%s://%s:%d", scheme, host, spec.SchemaRegistryPort())},
		TLS:  tls,
	}

	if spec.Auth.IsSASLEnabled() {
		sr.SASL = &ir.SchemaRegistrySASL{
			Username: redpandav1alpha2.StretchClusterBootstrapUsername,
			Password: stretchBootstrapPasswordSource(sc),
		}
	}

	return sr
}

// stretchListener pulls a listener out of spec.Listeners via the supplied
// getter, returning nil safely if Listeners is unset.
func stretchListener(spec *redpandav1alpha2.StretchClusterSpec, get func(*redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener) *redpandav1alpha2.StretchAPIListener {
	if spec == nil || spec.Listeners == nil {
		return nil
	}
	return get(spec.Listeners)
}

// stretchListenerTLS builds the CommonTLS describing where the CA cert can
// be loaded from, matching how Factory.stretchClusterListenerTLSConfig
// resolves the secret at runtime.
func stretchListenerTLS(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, listener *redpandav1alpha2.StretchAPIListener) *ir.CommonTLS {
	certName := listener.TLS.GetCert()
	if certName == "" {
		certName = "default"
	}

	caSecretName, caKey, _ := spec.TLS.CertificatesFor(sc.Name, certName)

	return &ir.CommonTLS{
		CaCert: &ir.ValueSource{
			Namespace: sc.Namespace,
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: caSecretName},
				Key:                  caKey,
			},
		},
	}
}

// stretchBootstrapPasswordSource returns the ValueSource pointing at the
// per-cluster bootstrap user secret.
func stretchBootstrapPasswordSource(sc *redpandav1alpha2.StretchCluster) *ir.ValueSource {
	return &ir.ValueSource{
		Namespace: sc.Namespace,
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: sc.BootstrapUserSecretName(),
			},
			Key: redpandav1alpha2.StretchClusterBootstrapPasswordKey,
		},
	}
}

// stretchSASLMechanism returns the configured SASL mechanism, defaulting
// where the spec leaves it unset.
func stretchSASLMechanism(spec *redpandav1alpha2.StretchClusterSpec) string {
	if spec == nil || spec.Auth == nil || spec.Auth.SASL == nil {
		return redpandav1alpha2.DefaultSASLMechanism
	}
	return spec.Auth.SASL.GetMechanism()
}
