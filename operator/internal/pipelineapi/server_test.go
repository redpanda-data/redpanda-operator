// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipelineapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeKeyPair writes a self-signed certificate for localhost and returns its
// PEM.
func writeKeyPair(t *testing.T, certFile, keyFile string, serial int64) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,

		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certPEM
}

func TestCertReloader(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	writeKeyPair(t, certFile, keyFile, 1)

	r := &certReloader{certFile: certFile, keyFile: keyFile}
	serial := func() int64 {
		cert, err := r.getCertificate(nil)
		require.NoError(t, err)
		leaf, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)
		return leaf.SerialNumber.Int64()
	}
	require.EqualValues(t, 1, serial())

	// A renewal is picked up once the check interval passes.
	writeKeyPair(t, certFile, keyFile, 2)
	require.NoError(t, os.Chtimes(certFile, time.Now(), time.Now().Add(time.Minute)))
	require.EqualValues(t, 1, serial())
	r.checked = time.Time{}
	require.EqualValues(t, 2, serial())

	// A broken file keeps the previous key pair.
	require.NoError(t, os.WriteFile(keyFile, []byte("garbage"), 0o600))
	require.NoError(t, os.Chtimes(certFile, time.Now(), time.Now().Add(2*time.Minute)))
	r.checked = time.Time{}
	require.EqualValues(t, 2, serial())

	// But one at startup is an error.
	_, err := (&certReloader{certFile: certFile, keyFile: keyFile}).getCertificate(nil)
	require.Error(t, err)
}

func TestServeTLS(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	certPEM := writeKeyPair(t, certFile, keyFile, 1)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, Handler(&Service{}), ServeOptions{Address: addr, CertFile: certFile, KeyFile: keyFile})
	}()

	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		resp, err := client.Get("https://" + addr + "/healthz")
		if !assert.NoError(c, err) {
			return
		}
		defer resp.Body.Close()
		assert.Equal(c, http.StatusOK, resp.StatusCode)
	}, 10*time.Second, 50*time.Millisecond)

	cancel()
	require.NoError(t, <-errCh)

	// A missing key pair fails at startup.
	require.Error(t, Serve(t.Context(), Handler(&Service{}), ServeOptions{Address: addr, CertFile: filepath.Join(dir, "missing.crt"), KeyFile: keyFile}))
}
