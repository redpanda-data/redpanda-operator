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

func (f *fakeStore) getTags(_ context.Context, key string) (map[string]string, error) {
	return f.tags[key], nil
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
