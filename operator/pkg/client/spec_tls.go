// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"

	"sigs.k8s.io/controller-runtime/pkg/log"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/redpanda"
)

func wrapTLSDialer(dialer redpanda.DialContextFunc, config *tls.Config) redpanda.DialContextFunc {
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		conn, err := dialer(ctx, network, host)
		if err != nil {
			return nil, err
		}

		serverName, _, err := net.SplitHostPort(host)
		if err != nil {
			// we likely didn't have a port, use
			// the whole string as the serverName
			serverName = host
		}

		config = config.Clone()
		if config.ServerName == "" {
			config.ServerName = serverName
		}

		return tls.Client(conn, config), nil
	}
}

func (c *Factory) configureSpecTLS(ctx context.Context, namespace string, spec *redpandav1alpha2.CommonTLS) (*tls.Config, error) {
	var caCertPool *x509.CertPool

	logger := log.FromContext(ctx)

	// Root CA
	if spec.CaCert != nil {
		ca, err := spec.CaCert.GetValue(ctx, c.Client, namespace, "ca.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read ca certificate secret: %w", err)
		}

		caCertPool = x509.NewCertPool()
		isSuccessful := caCertPool.AppendCertsFromPEM(ca)
		if !isSuccessful {
			logger.Info("failed to append ca file to cert pool, is this a valid PEM format?")
		}
	}

	// If configured load TLS cert & key - Mutual TLS
	var certificates []tls.Certificate
	if spec.Cert != nil && spec.Key != nil {
		// 1. Read certificates
		cert, err := spec.Cert.GetValue(ctx, c.Client, namespace, "tls.crt")
		if err != nil {
			return nil, fmt.Errorf("failed to read certificate secret: %w", err)
		}

		certData := cert

		key, err := spec.Cert.GetValue(ctx, c.Client, namespace, "tls.key")
		if err != nil {
			return nil, fmt.Errorf("failed to read key certificate secret: %w", err)
		}

		keyData := key

		// 2. Check if private key needs to be decrypted. Decrypt it if passphrase is given, otherwise return error
		pemBlock, _ := pem.Decode(keyData)
		if pemBlock == nil {
			return nil, fmt.Errorf("no valid private key found") // nolint:goerr113 // this error will not be handled by operator
		}

		tlsCert, err := tls.X509KeyPair(certData, keyData)
		if err != nil {
			return nil, fmt.Errorf("cannot parse pem: %w", err)
		}
		certificates = []tls.Certificate{tlsCert}
	}

	return &tls.Config{
		//nolint:gosec // InsecureSkipVerify may be true upon user's responsibility.
		InsecureSkipVerify: spec.InsecureSkipTLSVerify,
		Certificates:       certificates,
		RootCAs:            caCertPool,
	}, nil
}
