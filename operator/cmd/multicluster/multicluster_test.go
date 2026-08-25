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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// validBaseOptions returns a MulticlusterOptions that passes validate() so a
// single field under test can be varied in isolation.
func validBaseOptions() MulticlusterOptions {
	return MulticlusterOptions{
		Name:                       "node-a",
		Address:                    "10.0.0.1:9999",
		CAFile:                     "/etc/raft/ca.crt",
		PrivateKeyFile:             "/etc/raft/tls.key",
		CertificateFile:            "/etc/raft/tls.crt",
		BaseImage:                  "redpanda",
		BaseTag:                    "v25.1.1",
		PostRestartCaughtUpPercent: 100,
	}
}

// TestValidatePostRestartCaughtUpPercent covers the StretchCluster port of the
// roll-loop safety: like the single-cluster `run` command, the multicluster
// command rejects --post-restart-caught-up-percent outside [1,100] (>100 stalls
// the rolling restart forever, 0 silently disables the per-broker post-restart
// gate).
func TestValidatePostRestartCaughtUpPercent(t *testing.T) {
	t.Run("baseline is valid", func(t *testing.T) {
		o := validBaseOptions()
		require.NoError(t, o.validate())
	})

	for _, p := range []int{0, -1, 101, 200} {
		t.Run("rejects out-of-range value", func(t *testing.T) {
			o := validBaseOptions()
			o.PostRestartCaughtUpPercent = p
			err := o.validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "post-restart-caught-up-percent")
		})
	}

	for _, p := range []int{1, 50, 100} {
		t.Run("accepts in-range value", func(t *testing.T) {
			o := validBaseOptions()
			o.PostRestartCaughtUpPercent = p
			require.NoError(t, o.validate())
		})
	}
}

// TestMetricsOptionsFlagDefault pins the flag default that the chart's
// ServiceMonitor depends on. The rendered ServiceMonitor scrapes the metrics
// port with `scheme: https` and `insecureSkipVerify`, which only works if the
// metrics server terminates TLS; controller-runtime does that with a
// self-signed certificate whenever SecureServing is set and no certificate
// path is supplied.
func TestMetricsOptionsFlagDefault(t *testing.T) {
	cmd := Command()
	require.NoError(t, cmd.Flags().Parse(nil))

	secure, err := cmd.Flags().GetBool("metrics-secure")
	require.NoError(t, err)
	assert.True(t, secure, "--metrics-secure must default to true, or ServiceMonitor scrapes fail the TLS handshake")
}

func TestMetricsOptions(t *testing.T) {
	ctx := context.Background()
	logger := testr.New(t)

	// nextProtos reports the ALPN protocols the built TLSOpts settle on,
	// which is how the HTTP/2 opt-out is observable.
	nextProtos := func(options *metricsserver.Options) []string {
		config := &tls.Config{NextProtos: []string{"h2"}} //nolint:gosec // no MinVersion needed; only TLSOpts' effect is under test
		for _, opt := range options.TLSOpts {
			opt(config)
		}
		return config.NextProtos
	}

	t.Run("no bind address disables the metrics server", func(t *testing.T) {
		o := validBaseOptions()
		o.MetricsSecure = true

		options, err := o.metricsOptions(ctx, logger)
		require.NoError(t, err)
		assert.Nil(t, options)
	})

	t.Run("serves TLS by default", func(t *testing.T) {
		o := validBaseOptions()
		o.MetricsBindAddress = ":8443"
		o.MetricsSecure = true

		options, err := o.metricsOptions(ctx, logger)
		require.NoError(t, err)
		require.NotNil(t, options)
		assert.Equal(t, ":8443", options.BindAddress)
		assert.True(t, options.SecureServing)
		assert.NotNil(t, options.FilterProvider, "metrics must stay behind authn/authz")
		// No certificate path: controller-runtime generates a self-signed one.
		assert.Equal(t, []string{"http/1.1"}, nextProtos(options), "HTTP/2 must be disabled")
	})

	t.Run("opting out of TLS keeps authn/authz", func(t *testing.T) {
		o := validBaseOptions()
		o.MetricsBindAddress = ":8443"
		o.MetricsSecure = false

		options, err := o.metricsOptions(ctx, logger)
		require.NoError(t, err)
		require.NotNil(t, options)
		assert.False(t, options.SecureServing)
		assert.NotNil(t, options.FilterProvider, "--metrics-secure=false must not also drop authentication")
		assert.Empty(t, options.TLSOpts)
	})

	t.Run("a certificate path implies TLS", func(t *testing.T) {
		dir := t.TempDir()
		certPath, keyPath := writeSelfSignedKeyPair(t, dir)

		o := validBaseOptions()
		o.MetricsBindAddress = ":8443"
		// Explicitly off: the certificate flags still win, as their help says.
		o.MetricsSecure = false
		o.MetricsCertPath = certPath
		o.MetricsKeyPath = keyPath

		options, err := o.metricsOptions(ctx, logger)
		require.NoError(t, err)
		require.NotNil(t, options)
		assert.True(t, options.SecureServing)

		config := &tls.Config{} //nolint:gosec // no MinVersion needed; only TLSOpts' effect is under test
		for _, opt := range options.TLSOpts {
			opt(config)
		}
		assert.Equal(t, []string{"http/1.1"}, config.NextProtos)
		require.NotNil(t, config.GetCertificate, "the certificate watcher must be wired up")
	})

	t.Run("an unreadable certificate is a startup error", func(t *testing.T) {
		o := validBaseOptions()
		o.MetricsBindAddress = ":8443"
		o.MetricsCertPath = filepath.Join(t.TempDir(), "absent.crt")
		o.MetricsKeyPath = filepath.Join(t.TempDir(), "absent.key")

		_, err := o.metricsOptions(ctx, logger)
		require.Error(t, err)
	})
}

// writeSelfSignedKeyPair writes a throwaway certificate/key pair into dir and
// returns their paths. certwatcher.New insists on a loadable pair.
func writeSelfSignedKeyPair(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600))

	return certPath, keyPath
}
