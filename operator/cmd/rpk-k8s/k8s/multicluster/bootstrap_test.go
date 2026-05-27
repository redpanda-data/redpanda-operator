// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/redpanda-data/common-go/goldenfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster"
)

func threeClusterYAMLConfig() multicluster.BootstrapConfig {
	return multicluster.BootstrapConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "redpanda",
			ServiceName: "operator",
			Contexts:    []string{"cluster-a", "cluster-b", "cluster-c"},
		},
		Organization: "TestOrg",
		DNSOverrides: []string{
			"cluster-a=a.example.com",
			"cluster-b=b.example.com",
			"cluster-c=c.example.com",
		},
		TLS:      true,
		CreateNS: true,
		Output:   "yaml",
	}
}

// TestBootstrapRun_OutputYAML_Golden compares the rendered manifest stream
// against testdata/bootstrap_output_yaml.golden.yaml. Cert byte fields
// (ca.crt, tls.crt, tls.key) are redacted before comparison because Go's
// crypto stdlib intentionally adds non-determinism to keying/signing — see
// crypto/internal/randutil.MaybeReadByte and the Go 1.26 GenerateKey doc
// comment ("the Reader is ignored unless GODEBUG=cryptocustomrand=1 is
// set"). A deterministic-seed approach would flake on every run. SAN
// content is covered separately by TestBootstrapRun_OutputYAML_SANs.
func TestBootstrapRun_OutputYAML_Golden(t *testing.T) {
	cfg := threeClusterYAMLConfig()

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	redacted := redactCertBytes(buf.Bytes())
	goldenPath := filepath.Join("testdata", "bootstrap_output_yaml.golden.yaml")

	if goldenfile.Update() {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, redacted, 0o644))
		return
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "reading golden file (run with -update-golden to create)")
	assert.Equal(t, string(want), string(redacted))
}

// TestBootstrapRun_OutputYAML_SANs is a focused assertion on the property
// the --dns-override / --loadbalancer plumbing exists to control: each
// cluster's TLS leaf carries the expected SAN. Kept separate from the
// golden test so a SAN regression produces a targeted failure rather than
// a sea of byte-diff output.
func TestBootstrapRun_OutputYAML_SANs(t *testing.T) {
	cfg := threeClusterYAMLConfig()

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	certs := extractTLSLeafCerts(t, buf.Bytes())
	require.Len(t, certs, 3)

	got := map[string]bool{}
	for _, c := range certs {
		for _, n := range c.DNSNames {
			got[n] = true
		}
	}
	assert.True(t, got["a.example.com"], "cluster-a leaf missing SAN a.example.com (got %v)", got)
	assert.True(t, got["b.example.com"], "cluster-b leaf missing SAN b.example.com (got %v)", got)
	assert.True(t, got["c.example.com"], "cluster-c leaf missing SAN c.example.com (got %v)", got)
}

func TestBootstrapRun_OutputYAML_LoadBalancer_Golden(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.ProvisionLoadBalancers = true

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	goldenPath := filepath.Join("testdata", "bootstrap_output_yaml_loadbalancer.golden.yaml")
	if goldenfile.Update() {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, buf.Bytes(), 0o644))
		return
	}

	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "reading golden file (run with -update-golden to create)")
	assert.Equal(t, string(want), buf.String())
}

func TestBootstrapRun_OutputYAML_NameOverride(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.Connection.NameOverrides = []string{
		"cluster-a=custom-prefix-a",
		"cluster-b=custom-prefix-b",
	}

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	out := buf.String()
	assert.Contains(t, out, "name: custom-prefix-a-multicluster-certificates")
	assert.Contains(t, out, "name: custom-prefix-b-multicluster-certificates")
	assert.Contains(t, out, "name: cluster-c-multicluster-certificates")
}

func TestBootstrapRun_OutputYAML_NameOverride_LoadBalancer(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.ProvisionLoadBalancers = true
	cfg.Connection.NameOverrides = []string{
		"cluster-a=custom-a",
	}

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	out := buf.String()
	assert.Contains(t, out, "name: custom-a-multicluster-peer")
	assert.Contains(t, out, "name: cluster-b-multicluster-peer")
}

func TestBootstrapRun_OutputYAML_RejectsLBWithoutContext(t *testing.T) {
	cfg := multicluster.BootstrapConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "redpanda",
			ServiceName: "operator",
		},
		ProvisionLoadBalancers: true,
		Output:                 "yaml",
	}

	err := cfg.Run(context.Background(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--context")
}

func TestBootstrapRun_OutputYAML_RejectsMissingDNSOverride(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.DNSOverrides = nil

	err := cfg.Run(context.Background(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--dns-override")
}

func TestBootstrapRun_OutputYAML_RejectsUnknownFormat(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.Output = "json"

	err := cfg.Run(context.Background(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"yaml"`)
}

func TestBootstrapRun_OutputYAML_RejectsEmptyOutput(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	cfg.TLS = false
	cfg.CreateNS = false

	err := cfg.Run(context.Background(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of --tls or --create-namespace")
}

func TestBootstrapRun_OutputYAML_NoKubeconfigNeeded(t *testing.T) {
	cfg := multicluster.BootstrapConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "redpanda",
			ServiceName: "operator",
		},
		Organization: "TestOrg",
		DNSOverrides: []string{
			"cluster-a=a.example.com",
		},
		TLS:      true,
		CreateNS: true,
		Output:   "yaml",
	}

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))
	assert.Contains(t, buf.String(), "cluster-a")
}

var certDataLineRE = regexp.MustCompile(`(?m)^(\s+(?:ca\.crt|tls\.crt|tls\.key):\s+)\S+$`)

func redactCertBytes(data []byte) []byte {
	return certDataLineRE.ReplaceAll(data, []byte("${1}<REDACTED>"))
}

// extractTLSLeafCerts parses the YAML stream and returns the x509
// certificates encoded in each Secret's tls.crt field.
func extractTLSLeafCerts(t *testing.T, data []byte) []*x509.Certificate {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s+tls\.crt:\s+(\S+)$`)
	matches := re.FindAllSubmatch(data, -1)
	out := make([]*x509.Certificate, 0, len(matches))
	for _, m := range matches {
		raw, err := base64.StdEncoding.DecodeString(string(m[1]))
		require.NoError(t, err, "decoding base64 tls.crt")
		block, _ := pem.Decode(raw)
		require.NotNil(t, block, "decoding PEM tls.crt")
		c, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err, "parsing x509 tls.crt")
		out = append(out, c)
	}
	return out
}
