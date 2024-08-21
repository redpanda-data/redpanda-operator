package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"

	"github.com/redpanda-data/helm-charts/pkg/redpanda"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func wrapTLSDialer(dialer redpanda.DialContextFunc, config *tls.Config) redpanda.DialContextFunc {
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		conn, err := dialer(ctx, network, host)
		if err != nil {
			return nil, err
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
