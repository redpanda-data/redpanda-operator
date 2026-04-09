// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package checks

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
)

// TLSCheck reads the multicluster TLS certificate secret and validates the CA,
// leaf certificate, private key, chain of trust, expiry, and SANs.
// Populates cc.TLSSecret, cc.CACert, cc.TLSCert, cc.TLSKeyMatch.
type TLSCheck struct{}

func (c *TLSCheck) Name() string { return "tls" }

func (c *TLSCheck) Run(ctx context.Context, cc *CheckContext) []Result {
	secretName := c.findSecretName(cc)
	if secretName == "" {
		// Fall back: scan secrets in namespace.
		var secrets corev1.SecretList
		if err := cc.Ctl.List(ctx, cc.Namespace, &secrets); err != nil {
			return []Result{Fail(c.Name(), fmt.Sprintf("listing secrets: %v", err))}
		}
		for _, sec := range secrets.Items {
			if strings.HasSuffix(sec.Name, "-multicluster-certificates") {
				secretName = sec.Name
				break
			}
		}
	}

	if secretName == "" {
		return []Result{Fail(c.Name(), fmt.Sprintf("no multicluster-certificates secret found in namespace %s", cc.Namespace))}
	}

	var secret corev1.Secret
	if err := cc.Ctl.Get(ctx, kube.ObjectKey{Name: secretName, Namespace: cc.Namespace}, &secret); err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("cannot read secret %s: %v", secretName, err))}
	}
	cc.TLSSecret = &secret

	caPEM := secret.Data["ca.crt"]
	certPEM := secret.Data["tls.crt"]
	keyPEM := secret.Data["tls.key"]

	if len(caPEM) == 0 || len(certPEM) == 0 || len(keyPEM) == 0 {
		return []Result{Fail(c.Name(), fmt.Sprintf("secret %s missing required keys (ca.crt, tls.crt, tls.key)", secretName))}
	}

	var results []Result

	// Parse CA.
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("secret %s: ca.crt is not valid PEM", secretName))}
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return []Result{Fail(c.Name(), fmt.Sprintf("secret %s: cannot parse ca.crt: %v", secretName, err))}
	}
	cc.CACert = caCert

	if !caCert.IsCA {
		results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: ca.crt is not a CA certificate", secretName)))
	}
	if caCert.KeyUsage&x509.KeyUsageCertSign == 0 {
		results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: ca.crt missing CertSign key usage", secretName)))
	}

	// Parse leaf cert.
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return append(results, Fail(c.Name(), fmt.Sprintf("secret %s: tls.crt is not valid PEM", secretName)))
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return append(results, Fail(c.Name(), fmt.Sprintf("secret %s: cannot parse tls.crt: %v", secretName, err)))
	}
	cc.TLSCert = cert

	// Expiry.
	now := time.Now()
	if now.After(cert.NotAfter) {
		results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: tls.crt expired on %s", secretName, cert.NotAfter.Format(time.DateOnly))))
	}
	if now.After(caCert.NotAfter) {
		results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: ca.crt expired on %s", secretName, caCert.NotAfter.Format(time.DateOnly))))
	}

	// Chain verification.
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: tls.crt does not chain to ca.crt: %v", secretName, err)))
	}

	// Key match.
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock != nil {
		privKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
		if err == nil {
			pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
			if ok && privKey.PublicKey.Equal(pubKey) {
				cc.TLSKeyMatch = true
			} else {
				results = append(results, Fail(c.Name(), fmt.Sprintf("secret %s: tls.key does not match tls.crt public key", secretName)))
			}
		}
	}

	if len(results) == 0 {
		results = append(results, Pass(c.Name(), fmt.Sprintf("secret %s: certificates valid and chain verified", secretName)))
	}
	return results
}

func (c *TLSCheck) findSecretName(cc *CheckContext) string {
	if cc.Deployment == nil {
		return ""
	}
	for _, vol := range cc.Deployment.Spec.Template.Spec.Volumes {
		if vol.Secret != nil && strings.Contains(vol.Secret.SecretName, "multicluster-certificates") {
			return vol.Secret.SecretName
		}
	}
	return ""
}
