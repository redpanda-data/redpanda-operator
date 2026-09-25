// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// poolPKI resolves a pool's certificates and their keypairs. Infallible,
// which is what lets the volume, mount and config paths share it;
// [certificatesForPool] layers on the issuance requests, which can fail.
func poolPKI(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) redpanda.PKI {
	return redpanda.PKI{
		Namespace:    state.namespace,
		Labels:       state.commonLabels(),
		Certificates: poolCertificates(&pool.Spec, state.poolFullname(pool)),
	}
}

// poolCertificates is a pool's in-use certificates, sorted, with Client
// populated for those a listener requires mTLS on.
//
// NB: reads the spec, not the resolved listeners -- those take a
// [redpanda.PKI], so deriving one from them would be circular. Issuance is
// therefore a superset of serving: a listener with tls.enabled false has a
// certificate mounted while naming none of it.
func poolCertificates(spec *redpandav1alpha2.BrokerPoolSpec, poolFullname string) map[string]redpanda.Certificate {
	clientCerts := map[string]bool{}
	for _, name := range spec.InUseClientCerts() {
		clientCerts[name] = true
	}

	certs := map[string]redpanda.Certificate{}
	for _, name := range spec.InUseServerCerts() {
		certs[name] = certificateFor(spec, poolFullname, name, clientCerts[name])
	}

	return certs
}

// certificateFor resolves one of the pool's certificates. Secret names are
// keyed on poolFullname so two pools using cert name "default" don't collide.
func certificateFor(spec *redpandav1alpha2.BrokerPoolSpec, poolFullname, name string, withClient bool) redpanda.Certificate {
	// NB: nil-tolerant on spec; the TLS helpers are nil-safe themselves.
	var tls *redpandav1alpha2.TLS
	if spec != nil {
		tls = spec.TLS
	}

	var ca *string
	if tls != nil {
		if data, ok := tls.Certs[name]; ok && data.IsCAEnabled() {
			ca = ptr.To("ca.crt")
		}
	}

	var client *redpanda.Keypair
	if withClient {
		client = &redpanda.Keypair{
			Name:   fmt.Sprintf("%s-client", name),
			Secret: corev1.LocalObjectReference{Name: tls.CertClientSecretName(poolFullname, name)},
		}
	}

	return redpanda.Certificate{
		Server: redpanda.Keypair{
			Name:   name,
			CA:     ca,
			Secret: corev1.LocalObjectReference{Name: tls.CertServerSecretName(poolFullname, name)},
		},
		Client: client,
	}
}
