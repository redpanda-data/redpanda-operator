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

// TestBootstrapRun_OutputYAML_Golden compares the per-cluster files written
// to --output-dir against testdata/bootstrap_output_yaml/<context>.yaml.
// Cert byte fields (ca.crt, tls.crt, tls.key) are redacted before
// comparison because Go's crypto stdlib intentionally adds non-determinism
// to keying/signing — see crypto/internal/randutil.MaybeReadByte and the
// Go 1.26 GenerateKey doc comment ("the Reader is ignored unless
// GODEBUG=cryptocustomrand=1 is set"). A deterministic-seed approach
// would flake on every run. SAN content is covered separately by
// TestBootstrapRun_OutputYAML_SANs.
func TestBootstrapRun_OutputYAML_Golden(t *testing.T) {
	outDir := t.TempDir()
	cfg := threeClusterYAMLConfig()
	cfg.OutputDir = outDir

	require.NoError(t, cfg.Run(context.Background(), io.Discard))

	goldenDir := filepath.Join("testdata", "bootstrap_output_yaml")
	if goldenfile.Update() {
		require.NoError(t, os.RemoveAll(goldenDir))
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))
	}

	for _, ctxName := range []string{"cluster-a", "cluster-b", "cluster-c"} {
		got, err := os.ReadFile(filepath.Join(outDir, ctxName+".yaml"))
		require.NoError(t, err, "reading per-cluster output %s", ctxName)
		redacted := redactCertBytes(got)
		goldenPath := filepath.Join(goldenDir, ctxName+".yaml")
		if goldenfile.Update() {
			require.NoError(t, os.WriteFile(goldenPath, redacted, 0o644))
			continue
		}
		want, err := os.ReadFile(goldenPath)
		require.NoError(t, err, "reading golden file %s (run with -update-golden to create)", goldenPath)
		assert.Equal(t, string(want), string(redacted), "cluster %s", ctxName)
	}
}

// TestBootstrapRun_OutputYAML_SANs is a focused assertion on the property
// the --dns-override / --loadbalancer plumbing exists to control: each
// cluster's TLS leaf carries the expected SAN. Kept separate from the
// golden test so a SAN regression produces a targeted failure rather than
// a sea of byte-diff output.
func TestBootstrapRun_OutputYAML_SANs(t *testing.T) {
	outDir := t.TempDir()
	cfg := threeClusterYAMLConfig()
	cfg.OutputDir = outDir

	require.NoError(t, cfg.Run(context.Background(), io.Discard))

	want := map[string]string{
		"cluster-a": "a.example.com",
		"cluster-b": "b.example.com",
		"cluster-c": "c.example.com",
	}
	for ctxName, expectedSAN := range want {
		data, err := os.ReadFile(filepath.Join(outDir, ctxName+".yaml"))
		require.NoError(t, err)
		certs := extractTLSLeafCerts(t, data)
		require.Len(t, certs, 1, "expected exactly one leaf cert in %s.yaml", ctxName)
		dns := certs[0].DNSNames
		assert.Contains(t, dns, expectedSAN, "%s leaf missing SAN (got %v)", ctxName, dns)
	}
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
	outDir := t.TempDir()
	cfg := threeClusterYAMLConfig()
	cfg.OutputDir = outDir
	cfg.Connection.NameOverrides = []string{
		"cluster-a=custom-prefix-a",
		"cluster-b=custom-prefix-b",
	}

	require.NoError(t, cfg.Run(context.Background(), io.Discard))

	aOut, err := os.ReadFile(filepath.Join(outDir, "cluster-a.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(aOut), "name: custom-prefix-a-multicluster-certificates")

	bOut, err := os.ReadFile(filepath.Join(outDir, "cluster-b.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(bOut), "name: custom-prefix-b-multicluster-certificates")

	cOut, err := os.ReadFile(filepath.Join(outDir, "cluster-c.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(cOut), "name: cluster-c-multicluster-certificates")
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
	outDir := t.TempDir()
	cfg := multicluster.BootstrapConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "redpanda",
			ServiceName: "operator",
		},
		Organization: "TestOrg",
		DNSOverrides: []string{
			"cluster-a=a.example.com",
		},
		TLS:       true,
		CreateNS:  true,
		Output:    "yaml",
		OutputDir: outDir,
	}

	require.NoError(t, cfg.Run(context.Background(), io.Discard))

	data, err := os.ReadFile(filepath.Join(outDir, "cluster-a.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "cluster-a-multicluster-certificates")
}

// TestBootstrapRun_OutputYAML_RejectsTLSWithoutOutputDir guards the security
// fix: if the TLS path could be streamed to stdout, applying the file to
// any single cluster would create every peer's tls.key on that cluster.
func TestBootstrapRun_OutputYAML_RejectsTLSWithoutOutputDir(t *testing.T) {
	cfg := threeClusterYAMLConfig()
	// OutputDir intentionally left empty.

	err := cfg.Run(context.Background(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--output-dir")
}

// TestBootstrapRun_OutputYAML_NamespaceOnly_Stream_Golden covers the only
// bootstrap-mode YAML path that is allowed to stream to stdout: namespaces
// only, no TLS material. The single golden file captures the full stdout
// output so a regression in stream framing (comment headers, '---'
// separators) produces a focused failure.
func TestBootstrapRun_OutputYAML_NamespaceOnly_Stream_Golden(t *testing.T) {
	cfg := multicluster.BootstrapConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "redpanda",
			ServiceName: "operator",
		},
		DNSOverrides: []string{
			"cluster-a=a.example.com",
			"cluster-b=b.example.com",
			"cluster-c=c.example.com",
		},
		CreateNS: true,
		Output:   "yaml",
	}

	var buf bytes.Buffer
	require.NoError(t, cfg.Run(context.Background(), &buf))

	goldenPath := filepath.Join("testdata", "bootstrap_output_yaml_namespace_only.golden.yaml")
	if goldenfile.Update() {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, buf.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "reading golden file (run with -update-golden to create)")
	assert.Equal(t, string(want), buf.String())
}

// TestBootstrapRun_OutputYAML_PerClusterIsolation pins the property that
// each cluster's output file contains only that cluster's TLS material.
// Without --output-dir, the previous behavior streamed every cluster's
// tls.key into a single document — applying it anywhere leaked all keys.
func TestBootstrapRun_OutputYAML_PerClusterIsolation(t *testing.T) {
	outDir := t.TempDir()
	cfg := threeClusterYAMLConfig()
	cfg.OutputDir = outDir

	require.NoError(t, cfg.Run(context.Background(), io.Discard))

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	for _, ctxName := range []string{"cluster-a", "cluster-b", "cluster-c"} {
		data, err := os.ReadFile(filepath.Join(outDir, ctxName+".yaml"))
		require.NoError(t, err)
		text := string(data)
		assert.Contains(t, text, ctxName+"-multicluster-certificates",
			"%s.yaml missing its own Secret", ctxName)
		for _, other := range []string{"cluster-a", "cluster-b", "cluster-c"} {
			if other == ctxName {
				continue
			}
			assert.NotContains(t, text, other+"-multicluster-certificates",
				"%s.yaml leaked %s Secret", ctxName, other)
		}
	}
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
