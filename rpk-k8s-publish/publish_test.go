// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeStore is an in-memory objectStore for tests.
type fakeStore struct {
	objects map[string][]byte
	tags    map[string]map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string][]byte{}, tags: map[string]map[string]string{}}
}

func (f *fakeStore) put(_ context.Context, key string, body []byte, tags map[string]string) error {
	f.objects[key] = body
	f.tags[key] = tags
	return nil
}

func (f *fakeStore) list(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeStore) head(_ context.Context, key string) (map[string]string, bool, error) {
	tags, ok := f.tags[key]
	return tags, ok, nil
}

func TestParsePlatformFromFilename(t *testing.T) {
	cases := []struct {
		name             string
		wantOS, wantArch string
		wantErr          bool
	}{
		{"rpk-k8s-linux-amd64", "linux", "amd64", false},
		{"rpk-k8s-darwin-arm64", "darwin", "arm64", false},
		{"rpk-k8s-windows-amd64.exe", "windows", "amd64", false},
		{"rpk-k8s-windows-arm64.exe", "windows", "arm64", false},
		{"not-a-plugin-binary", "", "", true},
		{"rpk-k8s-linux", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goos, goarch, err := parsePlatform(c.name)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got none", c.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if goos != c.wantOS || goarch != c.wantArch {
				t.Fatalf("got %s/%s, want %s/%s", goos, goarch, c.wantOS, c.wantArch)
			}
		})
	}
}

func TestTarGzSingleFile(t *testing.T) {
	inner := []byte("fake-binary-contents")
	archive, err := tarGz("rpk-k8s-linux-amd64", inner)
	if err != nil {
		t.Fatalf("tarGz: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next: %v", err)
	}
	if hdr.Name != "rpk-k8s-linux-amd64" {
		t.Fatalf("arcname = %q, want rpk-k8s-linux-amd64", hdr.Name)
	}
	got, _ := io.ReadAll(tr)
	if !bytes.Equal(got, inner) {
		t.Fatalf("inner mismatch")
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("expected exactly one file in the tar")
	}
}

func TestBuildManifest_IsLatestPicksMaxStable(t *testing.T) {
	store := newFakeStore()
	seed := func(version, goos, goarch, sha string) {
		key := "k8s/archives/" + version + "/redpanda-k8s-" + goos + "-" + goarch + ".tar.gz"
		store.objects[key] = []byte("x")
		store.tags[key] = map[string]string{
			tagBinaryName:   "redpanda-k8s",
			tagBinarySha256: sha,
			tagGOOS:         goos,
			tagGOARCH:       goarch,
			tagVersion:      version,
		}
	}
	seed("25.3.4", "linux", "amd64", "aaa")
	seed("25.3.5", "linux", "amd64", "bbb")
	seed("25.3.5", "darwin", "arm64", "ddd")
	seed("25.3.6-rc1", "linux", "amd64", "ccc")

	m, err := buildManifest(context.Background(), store, "k8s", "redpanda-k8s", "rpk-plugins.redpanda.com")
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}

	var latest string
	for _, a := range m.Archives {
		if a.IsLatest {
			if latest != "" {
				t.Fatalf("more than one archive marked is_latest")
			}
			latest = a.Version
		}
	}
	if latest != "25.3.5" {
		t.Fatalf("is_latest = %q, want 25.3.5", latest)
	}

	for _, a := range m.Archives {
		if a.Version == "25.3.5" {
			if len(a.Artifacts) != 2 {
				t.Fatalf("expected 2 artifacts for 25.3.5, got %d", len(a.Artifacts))
			}
			art, ok := a.Artifacts["linux-amd64"]
			if !ok {
				t.Fatalf("missing linux-amd64 artifact for 25.3.5")
			}
			wantPath := "https://rpk-plugins.redpanda.com/k8s/archives/25.3.5/redpanda-k8s-linux-amd64.tar.gz"
			if art.Path != wantPath {
				t.Fatalf("path = %q, want %q", art.Path, wantPath)
			}
			if art.Sha256 != "bbb" {
				t.Fatalf("sha = %q, want bbb", art.Sha256)
			}
			dartArt, ok := a.Artifacts["darwin-arm64"]
			if !ok {
				t.Fatalf("missing darwin-arm64 artifact for 25.3.5")
			}
			if dartArt.Sha256 != "ddd" {
				t.Fatalf("darwin-arm64 sha = %q, want ddd", dartArt.Sha256)
			}
		}
	}
}

func TestBuildManifest_RCOnly_NoLatest(t *testing.T) {
	store := newFakeStore()
	key := "k8s/archives/25.3.6-rc1/redpanda-k8s-linux-amd64.tar.gz"
	store.objects[key] = []byte("x")
	store.tags[key] = map[string]string{
		tagBinaryName:   "redpanda-k8s",
		tagBinarySha256: "eee",
		tagGOOS:         "linux",
		tagGOARCH:       "amd64",
		tagVersion:      "25.3.6-rc1",
	}

	m, err := buildManifest(context.Background(), store, "k8s", "redpanda-k8s", "rpk-plugins.redpanda.com")
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	if len(m.Archives) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(m.Archives))
	}
	if m.Archives[0].IsLatest {
		t.Fatalf("RC-only release should not be marked is_latest")
	}
}

func TestBuildManifest_SkipsForeignBinaries(t *testing.T) {
	store := newFakeStore()
	key := "k8s/archives/9.9.9/redpanda-connect-linux-amd64.tar.gz"
	store.objects[key] = []byte("x")
	store.tags[key] = map[string]string{
		tagBinaryName:   "redpanda-connect",
		tagBinarySha256: "zzz",
		tagGOOS:         "linux",
		tagGOARCH:       "amd64",
		tagVersion:      "9.9.9",
	}
	m, err := buildManifest(context.Background(), store, "k8s", "redpanda-k8s", "rpk-plugins.redpanda.com")
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	if len(m.Archives) != 0 {
		t.Fatalf("expected foreign binary skipped, got %d archives", len(m.Archives))
	}
}

func TestSha256Hex(t *testing.T) {
	got := sha256Hex([]byte("payload"))
	sum := sha256.Sum256([]byte("payload"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("sha256Hex = %q, want %q", got, want)
	}
}

func TestUploadArchives_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Write two fake plugin binaries with distinct contents.
	linuxContents := []byte("fake-linux-amd64-binary")
	windowsContents := []byte("fake-windows-amd64-binary")
	if err := os.WriteFile(filepath.Join(dir, "rpk-k8s-linux-amd64"), linuxContents, 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rpk-k8s-windows-amd64.exe"), windowsContents, 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// This non-plugin file must be ignored by uploadArchives.
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("docs"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := newFakeStore()
	expected := []string{"linux-amd64", "windows-amd64"}
	if err := uploadArchives(context.Background(), store, dir, "k8s", "redpanda-k8s", "25.3.5", expected); err != nil {
		t.Fatalf("uploadArchives: %v", err)
	}

	// Exactly two objects must have been uploaded (README.txt ignored).
	if len(store.objects) != 2 {
		t.Fatalf("expected 2 uploaded objects, got %d", len(store.objects))
	}

	type wantEntry struct {
		key      string
		goos     string
		goarch   string
		rawBytes []byte
	}
	wants := []wantEntry{
		{
			key:      "k8s/archives/25.3.5/redpanda-k8s-linux-amd64.tar.gz",
			goos:     "linux",
			goarch:   "amd64",
			rawBytes: linuxContents,
		},
		{
			key:      "k8s/archives/25.3.5/redpanda-k8s-windows-amd64.tar.gz",
			goos:     "windows",
			goarch:   "amd64",
			rawBytes: windowsContents,
		},
	}

	for _, w := range wants {
		body, ok := store.objects[w.key]
		if !ok {
			t.Fatalf("expected key %q not found in store", w.key)
		}
		tags := store.tags[w.key]

		// Verify tags.
		if got := tags[tagBinaryName]; got != "redpanda-k8s" {
			t.Errorf("[%s] tagBinaryName = %q, want redpanda-k8s", w.key, got)
		}
		if got := tags[tagVersion]; got != "25.3.5" {
			t.Errorf("[%s] tagVersion = %q, want 25.3.5", w.key, got)
		}
		if got := tags[tagGOOS]; got != w.goos {
			t.Errorf("[%s] tagGOOS = %q, want %q", w.key, got, w.goos)
		}
		if got := tags[tagGOARCH]; got != w.goarch {
			t.Errorf("[%s] tagGOARCH = %q, want %q", w.key, got, w.goarch)
		}
		// Critical invariant: sha256 tag must be the hash of the RAW binary,
		// NOT of the .tar.gz wrapper — this is what rpk uses to verify downloads.
		wantSha := sha256Hex(w.rawBytes)
		if got := tags[tagBinarySha256]; got != wantSha {
			t.Errorf("[%s] tagBinarySha256 = %q, want %q (sha of raw binary)", w.key, got, wantSha)
		}

		// Verify the uploaded body is a valid gzip+tar containing exactly one
		// file whose contents equal the raw binary.
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("[%s] gzip.NewReader: %v", w.key, err)
		}
		tr := tar.NewReader(gz)
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("[%s] tar.Next: %v", w.key, err)
		}
		if hdr.Name == "" {
			t.Errorf("[%s] tar entry has empty name", w.key)
		}
		got, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("[%s] io.ReadAll from tar: %v", w.key, err)
		}
		if !bytes.Equal(got, w.rawBytes) {
			t.Errorf("[%s] tar inner contents mismatch: got %q, want %q", w.key, got, w.rawBytes)
		}
		if _, err := tr.Next(); err != io.EOF {
			t.Fatalf("[%s] expected exactly one file in the tar", w.key)
		}
	}
}

// TestUploadArchives_MissingPlatform asserts the publisher refuses to publish
// (uploading nothing) when the local build is missing any expected platform —
// so a partial cross-compile can never produce a partially published version.
func TestUploadArchives_MissingPlatform(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rpk-k8s-linux-amd64"), []byte("only-linux"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store := newFakeStore()
	expected := []string{"linux-amd64", "darwin-arm64", "windows-amd64"}
	err := uploadArchives(context.Background(), store, dir, "k8s", "redpanda-k8s", "25.3.5", expected)
	if err == nil {
		t.Fatalf("expected error for missing platforms, got nil")
	}
	if !strings.Contains(err.Error(), "missing expected platform") {
		t.Fatalf("error = %v, want it to mention missing expected platforms", err)
	}
	// Fail fast: nothing should have been uploaded.
	if len(store.objects) != 0 {
		t.Fatalf("expected 0 uploads on validation failure, got %d", len(store.objects))
	}
}

// TestUploadArchives_ImmutableRerun asserts that re-running for an
// already-published version/platform is a no-op when the bytes are identical
// and a hard error when they differ (published artifacts are immutable).
func TestUploadArchives_ImmutableRerun(t *testing.T) {
	dir := t.TempDir()
	contents := []byte("binary-v1")
	if err := os.WriteFile(filepath.Join(dir, "rpk-k8s-linux-amd64"), contents, 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	key := "k8s/archives/25.3.5/redpanda-k8s-linux-amd64.tar.gz"
	expected := []string{"linux-amd64"}

	// Identical bytes already published -> skip, no new upload, no error.
	t.Run("identical sha is a no-op", func(t *testing.T) {
		store := newFakeStore()
		store.tags[key] = map[string]string{tagBinarySha256: sha256Hex(contents)}
		// no body in store.objects: a skip must not write one
		if err := uploadArchives(context.Background(), store, dir, "k8s", "redpanda-k8s", "25.3.5", expected); err != nil {
			t.Fatalf("uploadArchives: %v", err)
		}
		if _, wrote := store.objects[key]; wrote {
			t.Fatalf("expected no upload for identical sha, but object was written")
		}
	})

	// Different bytes already published -> hard error, no overwrite.
	t.Run("different sha is rejected", func(t *testing.T) {
		store := newFakeStore()
		store.tags[key] = map[string]string{tagBinarySha256: "deadbeef-different"}
		err := uploadArchives(context.Background(), store, dir, "k8s", "redpanda-k8s", "25.3.5", expected)
		if err == nil {
			t.Fatalf("expected error overwriting with different sha, got nil")
		}
		if !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("error = %v, want it to mention immutability", err)
		}
		if _, wrote := store.objects[key]; wrote {
			t.Fatalf("must not overwrite an existing object with different bytes")
		}
	})
}

func TestLessVersion_SemverOrder(t *testing.T) {
	in := []string{"25.3.10", "25.3.9", "25.4.0", "25.3.5", "25.3.5-rc1", "25.3.5-rc2"}
	want := []string{"25.3.5-rc1", "25.3.5-rc2", "25.3.5", "25.3.9", "25.3.10", "25.4.0"}
	got := append([]string(nil), in...)
	sort.Slice(got, func(i, j int) bool { return lessVersion(got[i], got[j]) })
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted order = %v, want %v", got, want)
		}
	}
}

// TestBuildManifest_ArchiveOrder asserts the manifest archives are emitted in
// semver order (9 before 10), not lexical order.
func TestBuildManifest_ArchiveOrder(t *testing.T) {
	store := newFakeStore()
	seed := func(version string) {
		key := "k8s/archives/" + version + "/redpanda-k8s-linux-amd64.tar.gz"
		store.objects[key] = []byte("x")
		store.tags[key] = map[string]string{
			tagBinaryName:   "redpanda-k8s",
			tagBinarySha256: "s",
			tagGOOS:         "linux",
			tagGOARCH:       "amd64",
			tagVersion:      version,
		}
	}
	seed("25.3.9")
	seed("25.3.10")
	seed("25.3.5")

	m, err := buildManifest(context.Background(), store, "k8s", "redpanda-k8s", "rpk-plugins.redpanda.com")
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	var order []string
	for _, a := range m.Archives {
		order = append(order, a.Version)
	}
	want := []string{"25.3.5", "25.3.9", "25.3.10"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("archive order = %v, want %v", order, want)
		}
	}
}
