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
	"archive/zip"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/rpk-k8s/k8s/multicluster/checks"
)

// bundleWriter wraps a zip.Writer with helpers that serialise the structured
// state accumulated by the check pipeline into the bundle file tree. Errors
// from individual writes are returned to the caller so the caller can
// accumulate them in errors.txt rather than failing the whole bundle.
type bundleWriter struct {
	zw *zip.Writer
}

func newBundleWriter(w io.Writer) *bundleWriter {
	return &bundleWriter{zw: zip.NewWriter(w)}
}

func (b *bundleWriter) Close() error { return b.zw.Close() }

// writeBytes writes `data` to the zip at the given path. Empty paths are
// rejected as a programming error.
func (b *bundleWriter) writeBytes(p string, data []byte) error {
	if p == "" {
		return fmt.Errorf("bundleWriter: empty path")
	}
	f, err := b.zw.Create(p)
	if err != nil {
		return fmt.Errorf("creating zip entry %s: %w", p, err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing zip entry %s: %w", p, err)
	}
	return nil
}

// writeJSON marshals `v` as indented JSON and writes it.
func (b *bundleWriter) writeJSON(p string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", p, err)
	}
	return b.writeBytes(p, append(data, '\n'))
}

// writeYAML marshals `v` as YAML (via sigs.k8s.io/yaml so JSON tags on
// Kubernetes types work) and writes it.
func (b *bundleWriter) writeYAML(p string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", p, err)
	}
	return b.writeBytes(p, data)
}

// writeText writes a string verbatim.
func (b *bundleWriter) writeText(p, s string) error {
	return b.writeBytes(p, []byte(s))
}

// writeManifest writes a top-level manifest.json describing the bundle's
// shape, the contexts that were diagnosed, and the redaction settings used.
// Future bundle readers (and any downstream tooling) need this to interpret
// the bundle without reverse-engineering its layout.
type bundleManifest struct {
	SchemaVersion      int       `json:"schemaVersion"`
	GeneratedAt        time.Time `json:"generatedAt"`
	Namespace          string    `json:"namespace"`
	ServiceName        string    `json:"serviceName"`
	IncludePrivateKeys bool      `json:"includePrivateKeys"`
	Clusters           []string  `json:"clusters"`
	// LogsCollected reports whether operator pod logs were collected on
	// this run. When true, LogsLimitBytes / LogsTailLines record the caps
	// applied (0 means no cap).
	LogsCollected  bool  `json:"logsCollected"`
	LogsLimitBytes int64 `json:"logsLimitBytes,omitempty"`
	LogsTailLines  int64 `json:"logsTailLines,omitempty"`
}

const bundleSchemaVersion = 1

func (b *bundleWriter) writeManifestFile(cfg *BundleConfig, contexts []*checks.CheckContext, generatedAt time.Time, logs LogsOptions) error {
	clusters := make([]string, 0, len(contexts))
	for _, cc := range contexts {
		clusters = append(clusters, cc.Context)
	}
	sort.Strings(clusters)
	m := bundleManifest{
		SchemaVersion:      bundleSchemaVersion,
		GeneratedAt:        generatedAt,
		Namespace:          cfg.Connection.Namespace,
		ServiceName:        cfg.Connection.ServiceName,
		IncludePrivateKeys: cfg.IncludePrivateKeys,
		Clusters:           clusters,
		LogsCollected:      !cfg.SkipLogs,
	}
	if !cfg.SkipLogs {
		m.LogsLimitBytes = logs.LimitBytes
		m.LogsTailLines = logs.TailLines
	}
	return b.writeJSON("manifest.json", m)
}

// writeStatusTable renders the same human-readable status table that the
// `status` command prints, plus the issues + cross-cluster sections, into a
// single status.txt at the bundle root.
func (b *bundleWriter) writeStatusTable(contexts []*checks.CheckContext, clusterResults [][]checks.Result, crossResults []checks.Result) error {
	var buf strings.Builder
	printStatusTable(&buf, contexts)
	printClusterResults(&buf, contexts, clusterResults)
	printCrossClusterResults(&buf, crossResults)
	return b.writeText("status.txt", buf.String())
}

// writeClusterArtifacts serialises the state accumulated on a single
// CheckContext (Pod, Deployment, TLS material, raft status) plus its check
// Results into clusters/<name>/.
//
// Any per-artifact serialisation error is returned wrapped; the caller
// records it in errors.txt and continues with the next cluster so a single
// bad context can't lose the whole bundle.
func (b *bundleWriter) writeClusterArtifacts(cc *checks.CheckContext, results []checks.Result, includePrivateKeys bool) []error {
	root := path.Join("clusters", cc.Context)
	var errs []error
	mark := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	mark(b.writeJSON(path.Join(root, "checks.json"), results))

	if cc.Pod != nil {
		mark(b.writeYAML(path.Join(root, "pod.yaml"), pruneObjectMeta(cc.Pod)))
	}
	if cc.Deployment != nil {
		mark(b.writeYAML(path.Join(root, "deployment.yaml"), pruneObjectMeta(cc.Deployment)))
	}
	if len(cc.DeployArgs) > 0 {
		mark(b.writeText(path.Join(root, "deploy-args.txt"), strings.Join(cc.DeployArgs, "\n")+"\n"))
	}

	if cc.CACert != nil {
		mark(b.writeBytes(path.Join(root, "tls", "ca.crt"), pemEncodeCert(cc.CACert)))
	}
	if cc.TLSCert != nil {
		mark(b.writeBytes(path.Join(root, "tls", "tls.crt"), pemEncodeCert(cc.TLSCert)))
	}
	if cc.TLSSecret != nil {
		mark(b.writeYAML(path.Join(root, "tls", "tls-secret.yaml"), redactSecret(cc.TLSSecret, includePrivateKeys)))
	}
	mark(b.writeText(path.Join(root, "tls", "tls-key-match.txt"), fmt.Sprintf("%v\n", cc.TLSKeyMatch)))

	if cc.RaftStatus != nil {
		mark(b.writeJSON(path.Join(root, "raft-status.json"), cc.RaftStatus))
	}

	return errs
}

// writeCrossClusterArtifacts writes the cross-cluster check Results.
func (b *bundleWriter) writeCrossClusterArtifacts(results []checks.Result) error {
	return b.writeJSON(path.Join("cross-cluster", "checks.json"), results)
}

// writeErrors writes accumulated non-fatal collection errors to errors.txt.
// Mirrors the convention from `rpk debug bundle`. Empty input writes nothing.
func (b *bundleWriter) writeErrors(messages []string) error {
	if len(messages) == 0 {
		return nil
	}
	return b.writeText("errors.txt", strings.Join(messages, "\n")+"\n")
}

// pemEncodeCert returns a PEM-encoded CERTIFICATE block for c.
func pemEncodeCert(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

// pruneObjectMeta strips noisy server-side metadata (managedFields, the
// server-generated resourceVersion, the self link) from a Kubernetes object
// before it's serialised into the bundle. The intent is reviewability: the
// fields removed are 90% of the visual noise in a kubectl get -o yaml dump
// and add nothing to a diagnostic.
func pruneObjectMeta[T client.Object](o T) T {
	out := o.DeepCopyObject().(T)
	out.SetManagedFields(nil)
	// Don't strip ResourceVersion / UID — they can be useful for ticket
	// cross-referencing. The big offender is managedFields.
	return out
}

// redactSecret returns a copy of s with sensitive data keys redacted, unless
// includePrivateKeys is set. The default redacts:
//
//   - tls.key (private key half of an mTLS cert)
//   - kubeconfig.yaml (peer-cluster credentials cached by the multicluster
//     operator; their presence here would let a bundle reader access every
//     peer cluster, defeating the point of running the bundle from a single
//     starting cluster).
//
// Returned object is safe to mutate and to serialise.
func redactSecret(s *corev1.Secret, includePrivateKeys bool) *corev1.Secret {
	out := s.DeepCopy()
	out.ManagedFields = nil
	if includePrivateKeys {
		return out
	}
	const redacted = "REDACTED"
	for _, key := range []string{"tls.key", "kubeconfig.yaml"} {
		if _, ok := out.Data[key]; ok {
			out.Data[key] = []byte(redacted)
		}
	}
	return out
}
