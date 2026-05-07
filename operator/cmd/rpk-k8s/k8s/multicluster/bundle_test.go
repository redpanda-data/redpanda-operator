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
	"archive/zip"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"sort"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/redpanda-data/common-go/kube/kubetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster"
	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
	mcpkg "github.com/redpanda-data/redpanda-operator/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/testutil"
)

// fastTestChecks is the subset of cluster checks used by bundle tests. It
// excludes TLSCheck (uses WaitFor which polls until the secret exists or
// the context is cancelled — hangs against a vanilla envtest), RaftCheck
// (port-forwards to the operator pod), TLSSANCheck and DeploymentRaftCheck
// (depend on TLS state TLSCheck would have populated). PodCheck and
// DeploymentCheck are still exercised so the bundle's per-cluster artifact
// serialisation path is meaningful when those resources do exist.
var fastTestChecks = []checks.ClusterCheck{
	&checks.PodCheck{},
	&checks.DeploymentCheck{},
}

// TestBundleRun_RoundTrip_NoDiscovery exercises the full Run pipeline using
// pre-populated Connections (no discovery) against a single envtest. It
// verifies that a successful run produces the expected file layout, that
// manifest.json carries the right fields, and that errors.txt is omitted
// when no errors accumulated.
func TestIntegrationBundleRun_RoundTrip(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx := testutil.Context(t)
	ctl := kubetest.NewEnv(t)

	// Frozen clock so manifest.json and the default output filename are
	// reproducible across runs.
	frozen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	cfg := &multicluster.BundleConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   "default",
			ServiceName: "operator",
			Connections: []multicluster.ClusterConnection{
				{Name: "self", Ctl: ctl, SecretPrefix: "self"},
			},
		},
		ClusterChecks: fastTestChecks,
		Now:           func() time.Time { return frozen },
	}

	var buf bytes.Buffer
	res, err := cfg.Run(ctx, &buf)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Contexts, 1, "without discovery, the roster is just the starting cluster")

	files := readZipFiles(t, buf.Bytes())

	// Expected files at the root.
	require.Contains(t, files, "manifest.json")
	require.Contains(t, files, "status.txt")
	require.Contains(t, files, "clusters/self/checks.json")
	require.Contains(t, files, "cross-cluster/checks.json")

	// errors.txt should NOT be present when no errors accumulated. The check
	// pipeline against an empty envtest will produce *failing* check Results
	// (no operator pod found, etc.) — those are recorded in checks.json, not
	// errors.txt. errors.txt is only for collection-side failures.
	assert.NotContains(t, files, "errors.txt")

	// manifest.json should round-trip with the expected fields.
	var m struct {
		SchemaVersion      int       `json:"schemaVersion"`
		GeneratedAt        time.Time `json:"generatedAt"`
		Namespace          string    `json:"namespace"`
		ServiceName        string    `json:"serviceName"`
		IncludePrivateKeys bool      `json:"includePrivateKeys"`
		Clusters           []string  `json:"clusters"`
	}
	require.NoError(t, json.Unmarshal(files["manifest.json"], &m))
	assert.Equal(t, 1, m.SchemaVersion)
	assert.True(t, m.GeneratedAt.Equal(frozen), "generatedAt should reflect the injected clock")
	assert.Equal(t, "default", m.Namespace)
	assert.Equal(t, "operator", m.ServiceName)
	assert.False(t, m.IncludePrivateKeys)
	assert.Equal(t, []string{"self"}, m.Clusters)

	// checks.json under clusters/<name>/ should be a JSON array (each entry
	// is a checks.Result). We don't assert on the exact contents because
	// they reflect the live check implementations, but the array shape and
	// presence of a check name is enough to confirm the writer round-trips.
	var clusterChecks []struct {
		Name    string `json:"Name"`
		OK      bool   `json:"OK"`
		Message string `json:"Message"`
	}
	require.NoError(t, json.Unmarshal(files["clusters/self/checks.json"], &clusterChecks))
	require.NotEmpty(t, clusterChecks, "checks.json should contain at least one check Result")
	for _, c := range clusterChecks {
		assert.NotEmpty(t, c.Name, "each Result should carry a Name")
	}
}

// TestIntegrationBundleDiscoverPeers exercises peer discovery against a real
// apiserver. It pre-populates two labelled cache Secrets on a starting
// envtest cluster — one well-formed pointing at a real second envtest, one
// malformed — and verifies that discoverPeers returns the well-formed peer
// as a working Connection and records the malformed one as a Warning rather
// than failing.
func TestIntegrationBundleDiscoverPeers(t *testing.T) {
	testutil.SkipIfNotIntegration(t)

	ctx := testutil.Context(t)

	starting := kubetest.NewEnv(t)
	peer := kubetest.NewEnv(t)

	const ns = "default"

	// Well-formed cache Secret pointing at the peer envtest.
	wellFormed := buildCacheSecret(t, "test-kubeconfig-peer-a", ns, "peer-a", peerKubeconfig(t, "peer-a", peer))
	require.NoError(t, starting.Create(ctx, wellFormed))

	// Malformed cache Secret: correctly labelled but missing the peer label,
	// so discovery should record a Warning and skip it.
	malformed := buildCacheSecret(t, "test-kubeconfig-peer-b", ns, "", []byte("not a kubeconfig"))
	delete(malformed.Labels, mcpkg.MulticlusterPeerLabel)
	require.NoError(t, starting.Create(ctx, malformed))

	// And a Secret in the namespace that's NOT a cache (no labels) — must
	// be ignored entirely by the label selector, not even appearing in
	// Warnings.
	require.NoError(t, starting.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns},
		Data:       map[string][]byte{"some-key": []byte("some-value")},
	}))

	cfg := &multicluster.BundleConfig{
		Connection: multicluster.ConnectionConfig{
			Namespace:   ns,
			ServiceName: "operator",
			Connections: []multicluster.ClusterConnection{
				{Name: "self", Ctl: starting, SecretPrefix: "self"},
			},
		},
		ClusterChecks: fastTestChecks,
		Now:           func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}

	var buf bytes.Buffer
	res, err := cfg.Run(ctx, &buf)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Roster: starting + peer-a. The malformed Secret must NOT have produced
	// a peer entry, but it MUST have produced an errors.txt warning.
	got := make([]string, 0, len(res.Contexts))
	for _, cc := range res.Contexts {
		got = append(got, cc.Context)
	}
	sort.Strings(got)
	require.Equal(t, []string{"peer-a", "self"}, got)

	// Errors must mention the malformed Secret by name (so the operator can
	// chase down which one is broken).
	require.NotEmpty(t, res.Errors, "malformed cache Secret should produce a non-fatal Warning")
	var sawMalformed bool
	for _, e := range res.Errors {
		if bytes.Contains([]byte(e), []byte("test-kubeconfig-peer-b")) {
			sawMalformed = true
			break
		}
	}
	assert.True(t, sawMalformed, "errors should reference the malformed Secret by name; got: %q", res.Errors)

	// errors.txt must be present in the zip.
	files := readZipFiles(t, buf.Bytes())
	require.Contains(t, files, "errors.txt")

	// Both the starting cluster and the discovered peer should each have
	// their own clusters/<name>/checks.json.
	require.Contains(t, files, "clusters/self/checks.json")
	require.Contains(t, files, "clusters/peer-a/checks.json")
}

// TestRedactSecret locks down the default redaction behaviour. It is
// deliberately a pure unit test (no envtest) so a regression in the
// redaction defaults fails fast in CI.
func TestRedactSecret(t *testing.T) {
	in := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: "noisy", Operation: metav1.ManagedFieldsOperationUpdate},
			},
		},
		Data: map[string][]byte{
			"tls.crt":         []byte("PUBLIC-CRT-DATA"),
			"tls.key":         []byte("PRIVATE-KEY-DATA"),
			"ca.crt":          []byte("CA-DATA"),
			"kubeconfig.yaml": []byte("PEER-CREDS"),
			"opaque":          []byte("opaque-value"),
		},
	}

	t.Run("default redacts private material and managed fields", func(t *testing.T) {
		out := multicluster.ExportRedactSecretForTest(in, false)
		assert.Empty(t, out.ManagedFields, "managedFields should be stripped unconditionally")
		assert.Equal(t, "REDACTED", string(out.Data["tls.key"]))
		assert.Equal(t, "REDACTED", string(out.Data["kubeconfig.yaml"]))
		// Public material survives.
		assert.Equal(t, "PUBLIC-CRT-DATA", string(out.Data["tls.crt"]))
		assert.Equal(t, "CA-DATA", string(out.Data["ca.crt"]))
		// Unrelated keys are untouched.
		assert.Equal(t, "opaque-value", string(out.Data["opaque"]))
		// Mutating the output must not affect the input.
		out.Data["tls.crt"] = []byte("clobbered")
		assert.Equal(t, "PUBLIC-CRT-DATA", string(in.Data["tls.crt"]),
			"redactSecret must return a deep copy")
	})

	t.Run("includePrivateKeys disables redaction", func(t *testing.T) {
		out := multicluster.ExportRedactSecretForTest(in, true)
		assert.Equal(t, "PRIVATE-KEY-DATA", string(out.Data["tls.key"]))
		assert.Equal(t, "PEER-CREDS", string(out.Data["kubeconfig.yaml"]))
		assert.Empty(t, out.ManagedFields, "managedFields are stripped regardless of includePrivateKeys")
	})
}

// readZipFiles returns a map of zip-internal path -> contents from a buffer
// containing a complete zip archive. Test helper.
func readZipFiles(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		require.NoError(t, err)
		out[f.Name] = data
	}
	return out
}

// buildCacheSecret returns a Secret with the labels written by the
// multicluster operator's writeCachedKubeconfig and the given peer name
// and kubeconfig payload.
func buildCacheSecret(t *testing.T, name, namespace, peerName string, payload []byte) *corev1.Secret {
	t.Helper()
	labels := map[string]string{
		mcpkg.KubeconfigCacheComponentLabel: mcpkg.KubeconfigCacheComponentValue,
		mcpkg.KubeconfigCacheManagedByLabel: mcpkg.KubeconfigCacheManagedByValue,
	}
	if peerName != "" {
		labels[mcpkg.MulticlusterPeerLabel] = peerName
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string][]byte{"kubeconfig.yaml": payload},
	}
}

// peerKubeconfig serialises a kube.Ctl's REST config as kubeconfig YAML
// bytes that LoadKubeconfigFromBytes can parse back into a *rest.Config.
// envtest uses client-cert auth, so we embed the cert/key/CA directly.
func peerKubeconfig(t *testing.T, name string, ctl *kube.Ctl) []byte {
	t.Helper()
	cfg := ctl.RestConfig()
	require.NotNil(t, cfg, "kube.Ctl must expose its REST config")
	return restConfigToKubeconfigYAML(t, name, cfg)
}

func restConfigToKubeconfigYAML(t *testing.T, name string, cfg *rest.Config) []byte {
	t.Helper()
	kc := clientcmdapi.NewConfig()
	kc.Clusters[name] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	kc.AuthInfos[name] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
	}
	kc.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
	kc.CurrentContext = name
	out, err := clientcmd.Write(*kc)
	require.NoError(t, err)
	return out
}

// generateTestCert is a small helper to build an x509.Certificate for the
// pure-Go redact / serialisation tests. Currently unused at the test level
// but kept so future tests for writeClusterArtifacts can populate
// CACert/TLSCert without spinning up cert-manager.
//
//nolint:unused // retained for forthcoming serialisation tests
func generateTestCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	c, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return c
}

// silence unused-import warnings while the helpers above carry stubs for
// future tests.
var _ = context.Background
