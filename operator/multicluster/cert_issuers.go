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
	"sort"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// bootstrappedCert holds the resolved cert name and duration for certs that
// need issuer/CA bootstrapping (i.e. not disabled, not user-provided, and
// without an external issuer).
type bootstrappedCert struct {
	name     string
	duration time.Duration
}

// bootstrappedCerts returns the set of in-use cert names (with durations) that
// require self-signed issuer and root CA bootstrapping. Names are the union
// across local pools' TLS configs; the duration for each name is the longest
// Duration any pool sets, falling back to defaultCertDuration.
func bootstrappedCerts(state *RenderState) []bootstrappedCert {
	names := BootstrappedCertNames(state.inClusterPools)
	var result []bootstrappedCert
	for _, name := range names {
		duration := defaultCertDuration
		for _, pool := range state.inClusterPools {
			if pool.Spec.TLS == nil {
				continue
			}
			if cert, ok := pool.Spec.TLS.Certs[name]; ok && cert != nil && cert.Duration != nil {
				if cert.Duration.Duration > duration {
					duration = cert.Duration.Duration
				}
			}
		}
		result = append(result, bootstrappedCert{name: name, duration: duration})
	}
	return result
}

// BootstrappedCertNames returns the sorted union of in-use cert names across
// the given local pools that require operator-managed CA bootstrapping. A
// cert is excluded only if for EVERY pool it's either disabled, has a
// user-provided SecretRef, or has an external IssuerRef. If any pool wants
// the bootstrapped CA-issued form for that cert name, it's included.
//
// Used by both the renderer (this package) and the multicluster controller's
// syncCA path so the union derivation has a single source of truth. Callers
// must pass pools whose specs already have MergeDefaults applied — both
// existing call sites do so (the renderer via RenderState, the controller
// via [lifecycle.StretchClusterWithPools.GetAllBrokerPools]).
func BootstrappedCertNames(pools []*redpandav1alpha2.RedpandaBrokerPool) []string {
	bootstrap := map[string]bool{}
	for _, pool := range pools {
		spec := &pool.Spec
		if spec.TLS == nil || !spec.TLS.IsEnabled() {
			continue
		}
		inUse := map[string]bool{}
		for _, name := range spec.InUseServerCerts() {
			inUse[name] = true
		}
		for _, name := range spec.InUseClientCerts() {
			inUse[name] = true
		}
		for name := range inUse {
			cert := spec.TLS.Certs[name]
			if cert != nil {
				if !cert.IsEnabled() || cert.SecretRef != nil || cert.IssuerRef != nil {
					continue
				}
			}
			bootstrap[name] = true
		}
	}
	sorted := make([]string, 0, len(bootstrap))
	for name := range bootstrap {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted
}

// CASecretName returns the well-known Secret name for a shared root CA,
// scoped to the StretchCluster name (not the per-k8s-cluster fullname).
func CASecretName(stretchClusterName, certName string) string {
	return fmt.Sprintf("%s-%s-root-certificate", stretchClusterName, certName)
}

// certIssuers returns all cert-manager CA Issuers for the given RenderState.
// Each bootstrapped cert gets a single CA Issuer backed by the shared root CA
// Secret that the multicluster reconciler distributes to all clusters. The Issuer is
// per-k8s-cluster (namespaced), but references the shared CA Secret whose name
// is scoped to the StretchCluster (not the per-cluster fullname).
func certIssuers(state *RenderState) []*certmanagerv1.Issuer {
	fullname := state.fullname()
	var issuers []*certmanagerv1.Issuer

	for _, bc := range bootstrappedCerts(state) {
		issuers = append(issuers, &certmanagerv1.Issuer{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "cert-manager.io/v1",
				Kind:       "Issuer",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-root-issuer", fullname, bc.name),
				Namespace: state.namespace,
				Labels:    state.commonLabels(),
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: CASecretName(state.cluster.Name, bc.name),
					},
				},
			},
		})
	}

	return issuers
}
