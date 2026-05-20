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
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

// ConvertStretchClusterToStaticConfig derives a StaticConfigurationSource —
// brokers, admin URLs, schema-registry URLs, TLS, and SASL — from a
// StretchCluster and a representative NodePool. The output mirrors
// what the legacy chart path produces via RenderState.AsStaticConfigSource
// for a Redpanda CR, so downstream callers (Console controller, in particular)
// can consume both kinds of cluster references through the same
// StaticConfigurationSource type.
//
// Per-K8s-cluster configuration (TLS, listener ports, ClusterDomain) lives
// on the NodePool spec after the field-move refactor; the caller fetches a
// representative pool and passes it in. The converter applies cluster and
// pool defaults internally, so the caller can pass the pool straight from
// the K8s API. SASL/auth and the cluster name itself are still read off
// the StretchCluster.
//
// The pool fullname (cluster name + pool suffix, the same convention the
// multicluster renderer uses) is threaded through to TLS resolution so
// that IssuerRef-backed leaf certificate Secrets — which cert-manager
// creates against the pool fullname — are referenced correctly. Without
// it, Console would look for `<cluster>-<cert>-cert` while the actual
// Secret is `<cluster>-<pool>-<cert>-cert`.
//
// All endpoint URLs point at the headless Service of the StretchCluster in
// the form `<name>.<namespace>.svc.<cluster-domain>:<port>`. This single
// endpoint reaches all brokers across all participating Kubernetes clusters
// because each cluster's local headless Service has EndpointSlices for
// every peer pod (under crossClusterMode=flat) or the equivalent for the
// other cross-cluster modes.
func ConvertStretchClusterToStaticConfig(sc *redpandav1alpha2.StretchCluster, pool *redpandav1alpha2.NodePool) *ir.StaticConfigurationSource {
	if sc == nil || pool == nil {
		return nil
	}

	spec := sc.Spec.DeepCopy()
	spec.MergeDefaults()

	poolSpec := pool.Spec.EmbeddedNodePoolSpec.DeepCopy()
	poolSpec.MergeDefaultsFrom(spec)

	poolFullname := tplutil.CleanForK8s(sc.Name) + pool.Suffix()

	host := strings.TrimSuffix(poolSpec.InternalDomain(sc.Name, sc.Namespace), ".")

	cfg := &ir.StaticConfigurationSource{
		Kafka: stretchKafkaSpec(sc, spec, poolSpec, poolFullname, host),
		Admin: stretchAdminSpec(sc, spec, poolSpec, poolFullname, host),
	}
	if sr := stretchSchemaRegistrySpec(sc, spec, poolSpec, poolFullname, host); sr != nil {
		cfg.SchemaRegistry = sr
	}

	return cfg
}

func stretchKafkaSpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, poolSpec *redpandav1alpha2.EmbeddedNodePoolSpec, poolFullname, host string) *ir.KafkaAPISpec {
	kafka := &ir.KafkaAPISpec{
		Brokers: []string{fmt.Sprintf("%s:%d", host, poolSpec.KafkaPort())},
	}

	if listener := stretchListener(poolSpec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener { return l.Kafka }); listener != nil && listener.IsTLSEnabled(poolSpec.TLS) {
		kafka.TLS = stretchListenerTLS(sc, poolSpec, poolFullname, listener)
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

func stretchAdminSpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, poolSpec *redpandav1alpha2.EmbeddedNodePoolSpec, poolFullname, host string) *ir.AdminAPISpec {
	scheme := "http"
	var tls *ir.CommonTLS
	if listener := stretchListener(poolSpec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener { return l.Admin }); listener != nil && listener.IsTLSEnabled(poolSpec.TLS) {
		scheme = "https"
		tls = stretchListenerTLS(sc, poolSpec, poolFullname, listener)
	}

	admin := &ir.AdminAPISpec{
		URLs: []string{fmt.Sprintf("%s://%s:%d", scheme, host, poolSpec.AdminPort())},
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

func stretchSchemaRegistrySpec(sc *redpandav1alpha2.StretchCluster, spec *redpandav1alpha2.StretchClusterSpec, poolSpec *redpandav1alpha2.EmbeddedNodePoolSpec, poolFullname, host string) *ir.SchemaRegistrySpec {
	listener := stretchListener(poolSpec, func(l *redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener {
		return l.SchemaRegistry
	})

	scheme := "http"
	var tls *ir.CommonTLS
	if listener != nil && listener.IsTLSEnabled(poolSpec.TLS) {
		scheme = "https"
		tls = stretchListenerTLS(sc, poolSpec, poolFullname, listener)
	}

	sr := &ir.SchemaRegistrySpec{
		URLs: []string{fmt.Sprintf("%s://%s:%d", scheme, host, poolSpec.SchemaRegistryPort())},
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

// stretchListener pulls a listener out of poolSpec.Listeners via the supplied
// getter, returning nil safely if Listeners is unset.
func stretchListener(poolSpec *redpandav1alpha2.EmbeddedNodePoolSpec, get func(*redpandav1alpha2.StretchListeners) *redpandav1alpha2.StretchAPIListener) *redpandav1alpha2.StretchAPIListener {
	if poolSpec == nil || poolSpec.Listeners == nil {
		return nil
	}
	return get(poolSpec.Listeners)
}

// stretchListenerTLS builds the CommonTLS describing where the CA cert can
// be loaded from, matching how Factory.stretchClusterListenerTLSConfig
// resolves the secret at runtime.
//
// CertificatesFor returns one of two Secret names depending on how the cert
// is sourced:
//
//   - Operator-managed (no SecretRef, no IssuerRef): the shared root-CA
//     Secret `<cluster-name>-<cert>-root-certificate`. That Secret lives at
//     cluster scope, so `sc.Name` is the right fullname to pass in.
//   - IssuerRef (user-supplied cert-manager Issuer): the leaf-cert Secret
//     that cert-manager creates from our rendered Certificate. Leaf
//     Certificates are rendered with the *pool* fullname
//     (`<cluster>-<pool>`), so we must pass `poolFullname` here. Without
//     it, Console would point at `<cluster>-<cert>-cert` and cert-manager
//     would write `<cluster>-<pool>-<cert>-cert`, leaving Console TLS
//     broken.
//   - SecretRef: returns the user-named Secret as-is, so the fullname
//     argument is ignored.
func stretchListenerTLS(sc *redpandav1alpha2.StretchCluster, poolSpec *redpandav1alpha2.EmbeddedNodePoolSpec, poolFullname string, listener *redpandav1alpha2.StretchAPIListener) *ir.CommonTLS {
	certName := listener.TLS.GetCert()
	if certName == "" {
		certName = redpandav1alpha2.DefaultCertName
	}

	fullname := sc.Name
	if cert, ok := poolSpec.TLS.Certs[certName]; ok && cert != nil && cert.IssuerRef != nil {
		fullname = poolFullname
	}
	caSecretName, caKey, _ := poolSpec.TLS.CertificatesFor(fullname, certName)

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
