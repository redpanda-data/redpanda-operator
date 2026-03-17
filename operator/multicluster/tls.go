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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
)

// TLSConfig constructs a tls.Config for the given TLS certificate name.
func (r *RenderState) TLSConfig(certName string) (*tls.Config, error) {
	if r.client == nil {
		return nil, fmt.Errorf("no kubernetes client available for TLS config lookup")
	}

	namespace := r.namespace
	serverName := r.Spec().InternalDomain(r.fullname(), r.namespace)

	rootCertName, rootCertKey, clientCertName := r.Spec().TLS.CertificatesFor(r.fullname(), certName)

	serverTLSError := func(err error) error {
		return fmt.Errorf("error fetching server root CA %s/%s: %w", namespace, rootCertName, err)
	}
	clientTLSError := func(err error) error {
		return fmt.Errorf("error fetching client certificate default/%s: %w", clientCertName, err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}

	var serverCert corev1.Secret
	lookupErr := r.client.Get(context.TODO(), kube.ObjectKey{Name: rootCertName, Namespace: namespace}, &serverCert)
	if lookupErr != nil {
		if k8sapierrors.IsNotFound(lookupErr) {
			return nil, serverTLSError(errServerCertificateNotFound)
		}
		return nil, serverTLSError(lookupErr)
	}

	serverPublicKey, found := serverCert.Data[rootCertKey]
	if !found {
		return nil, serverTLSError(errServerCertificatePublicKeyNotFound)
	}

	block, _ := pem.Decode(serverPublicKey)
	if block == nil {
		return nil, serverTLSError(fmt.Errorf("unable to decode PEM block from public key"))
	}
	serverParsedCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, serverTLSError(fmt.Errorf("unable to parse public key %w", err))
	}
	pool := x509.NewCertPool()
	pool.AddCert(serverParsedCertificate)

	tlsConfig.RootCAs = pool

	if r.Spec().Listeners.CertRequiresClientAuth(certName) {
		var clientCert corev1.Secret
		lookupErr := r.client.Get(context.TODO(), kube.ObjectKey{Name: clientCertName, Namespace: namespace}, &clientCert)
		if lookupErr != nil {
			if k8sapierrors.IsNotFound(lookupErr) {
				return nil, clientTLSError(errClientCertificateNotFound)
			}
			return nil, clientTLSError(lookupErr)
		}

		clientPublicKey, found := clientCert.Data[corev1.TLSCertKey]
		if !found {
			return nil, clientTLSError(errClientCertificatePublicKeyNotFound)
		}

		clientPrivateKey, found := clientCert.Data[corev1.TLSPrivateKeyKey]
		if !found {
			return nil, clientTLSError(errClientCertificatePrivateKeyNotFound)
		}

		clientKey, err := tls.X509KeyPair(clientPublicKey, clientPrivateKey)
		if err != nil {
			return nil, clientTLSError(fmt.Errorf("unable to parse public and private key %w", err))
		}

		tlsConfig.Certificates = []tls.Certificate{clientKey}
	}

	return tlsConfig, nil
}
