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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// binaryPrefix is the common prefix of the cross-compiled rpk-k8s binaries
// emitted by `task ci:build:rpk-k8s` (e.g. rpk-k8s-linux-amd64,
// rpk-k8s-windows-amd64.exe).
const binaryPrefix = "rpk-k8s-"

// S3 object tag keys — identical to connect's plugin_uploader so the layout
// and manifest semantics match across managed plugins.
const (
	tagBinaryName   = "redpanda/binary_name"
	tagBinarySha256 = "redpanda/binary_sha256"
	tagGOOS         = "redpanda/goos"
	tagGOARCH       = "redpanda/goarch"
	tagVersion      = "redpanda/version"
)

// stableVersionRe matches only pure X.Y.Z versions; pre-releases such as
// 25.3.5-rc1 do not match and are therefore never marked is_latest.
var stableVersionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// info is one published artifact discovered from S3 object tags.
type info struct{ goos, goarch, sha, path string }

// objectStore is the minimal S3 surface the publisher needs. The real
// implementation lives in s3.go; tests inject an in-memory fake.
type objectStore interface {
	put(ctx context.Context, key string, body []byte, tags map[string]string) error
	list(ctx context.Context, prefix string) ([]string, error)
	// head returns an object's tags and whether it exists. A missing object
	// is (nil, false, nil); a genuine lookup failure returns a non-nil error.
	head(ctx context.Context, key string) (tags map[string]string, exists bool, err error)
}

// RepoArtifact / RepoArchive / RepoManifest mirror the shape rpk's
// plugin.RepoManifest expects at <repo>/<slug>/manifest.json. created_at is
// informational, matching connect's manifest.
type RepoArtifact struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

type RepoArchive struct {
	Version   string                  `json:"version"`
	IsLatest  bool                    `json:"is_latest"`
	Artifacts map[string]RepoArtifact `json:"artifacts"`
}

type RepoManifest struct {
	CreatedAt int64         `json:"created_at"`
	Archives  []RepoArchive `json:"archives"`
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// parsePlatform extracts goos/goarch from a built binary's base name, e.g.
// "rpk-k8s-windows-amd64.exe" -> ("windows", "amd64").
func parsePlatform(name string) (goos, goarch string, err error) {
	base := strings.TrimSuffix(name, ".exe")
	rest, ok := strings.CutPrefix(base, binaryPrefix)
	if !ok {
		return "", "", fmt.Errorf("%q is not an rpk-k8s binary (missing %q prefix)", name, binaryPrefix)
	}
	parts := strings.Split(rest, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse goos/goarch from %q", name)
	}
	return parts[0], parts[1], nil
}

// tarGz wraps a single binary into a .tar.gz with the given arcname, matching
// what rpk's plugin.Download expects (gunzip -> untar one file -> sha256).
func tarGz(arcname string, binary []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: arcname,
		Mode: 0o755,
		Size: int64(len(binary)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(binary); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// discoverBinaries returns the rpk-k8s-* files in dir.
func discoverBinaries(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("unable to read binary dir %q: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), binaryPrefix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// foundBinary is a discovered local binary plus its parsed platform.
type foundBinary struct {
	path, goos, goarch string
}

// uploadArchives tar.gz's and uploads every rpk-k8s-* binary in binaryDir to
// <plugin>/archives/<version>/<binaryName>-<goos>-<goarch>.tar.gz, tagging
// each object so buildManifest can reconstruct the manifest later.
//
// expectedPlatforms is the set of "<goos>-<goarch>" the release must provide;
// the full set is validated before anything is uploaded, so a partial build
// (e.g. one cross-compile target failed) never results in a partially
// published version. Each archive is also immutable: re-uploading the same
// version/platform is a no-op if the bytes are identical and a hard error if
// they differ, so a retried or re-dispatched release can never silently
// replace already-published bytes.
func uploadArchives(ctx context.Context, store objectStore, binaryDir, plugin, binaryName, version string, expectedPlatforms []string) error {
	bins, err := discoverBinaries(binaryDir)
	if err != nil {
		return err
	}

	found := make([]foundBinary, 0, len(bins))
	have := map[string]bool{}
	for _, path := range bins {
		goos, goarch, err := parsePlatform(filepath.Base(path))
		if err != nil {
			return err
		}
		found = append(found, foundBinary{path: path, goos: goos, goarch: goarch})
		have[goos+"-"+goarch] = true
	}

	// Validate the full expected platform matrix BEFORE uploading anything.
	var missing []string
	for _, p := range expectedPlatforms {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("refusing to publish %s %s: missing expected platform binaries %v in %q (expected all of %v)", plugin, version, missing, binaryDir, expectedPlatforms)
	}

	for _, b := range found {
		raw, err := os.ReadFile(b.path)
		if err != nil {
			return fmt.Errorf("unable to read %q: %w", b.path, err)
		}
		sha := sha256Hex(raw)
		key := fmt.Sprintf("%s/archives/%s/%s-%s-%s.tar.gz", plugin, version, binaryName, b.goos, b.goarch)

		// Published version artifacts are immutable: skip if identical bytes
		// already exist, hard-fail if a different binary is already published
		// at this version/platform (a rebuild bakes a fresh buildDate, so a
		// rerun of the same tag would otherwise overwrite with new bytes).
		existing, exists, err := store.head(ctx, key)
		if err != nil {
			return fmt.Errorf("unable to check existing object %q: %w", key, err)
		}
		if exists {
			if existing[tagBinarySha256] == sha {
				fmt.Printf("skipping %s: already published with identical sha256=%s\n", key, sha)
				continue
			}
			return fmt.Errorf("refusing to overwrite %s: already published with sha256=%s but local binary has sha256=%s; published version artifacts are immutable", key, existing[tagBinarySha256], sha)
		}

		// The inner tar entry name is cosmetic: rpk's plugin installer untars the
		// single file and writes it to ~/.local/bin/.rpk.managed-<slug> regardless
		// of the entry name, so arcname need not match the S3 archive's redpanda-k8s-... basename.
		arcname := filepath.Base(strings.TrimSuffix(b.path, ".exe"))
		archive, err := tarGz(arcname, raw)
		if err != nil {
			return fmt.Errorf("unable to archive %q: %w", b.path, err)
		}
		tags := map[string]string{
			tagBinaryName:   binaryName,
			tagBinarySha256: sha,
			tagGOOS:         b.goos,
			tagGOARCH:       b.goarch,
			tagVersion:      version,
		}
		fmt.Printf("uploading %s (%s, sha256=%s)\n", key, b.goos+"/"+b.goarch, sha)
		if err := store.put(ctx, key, archive, tags); err != nil {
			return fmt.Errorf("unable to upload %q: %w", key, err)
		}
	}
	return nil
}

// buildManifest lists every archive under <plugin>/archives/, groups them by
// version from their object tags, and marks the max pure-stable X.Y.Z as
// is_latest. Listing from S3 (rather than only this run's binaries) preserves
// previously published versions.
func buildManifest(ctx context.Context, store objectStore, plugin, binaryName, repoHostname string) (*RepoManifest, error) {
	prefix := plugin + "/archives/"
	keys, err := store.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	byVersion := map[string][]info{}
	for _, key := range keys {
		tags, exists, err := store.head(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("unable to read tags for %q: %w", key, err)
		}
		if !exists {
			// Listed but vanished between list and head (eventual consistency
			// or concurrent delete); skip rather than fail the whole manifest.
			continue
		}
		if tags[tagBinaryName] != binaryName {
			continue
		}
		version := tags[tagVersion]
		if version == "" {
			continue
		}
		byVersion[version] = append(byVersion[version], info{
			goos:   tags[tagGOOS],
			goarch: tags[tagGOARCH],
			sha:    tags[tagBinarySha256],
			path:   key,
		})
	}

	maxStable := maxStableVersion(byVersion)

	manifest := &RepoManifest{Archives: []RepoArchive{}}
	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		artifacts := map[string]RepoArtifact{}
		for _, in := range byVersion[v] {
			artifacts[in.goos+"-"+in.goarch] = RepoArtifact{
				Path:   fmt.Sprintf("https://%s/%s", repoHostname, in.path),
				Sha256: in.sha,
			}
		}
		manifest.Archives = append(manifest.Archives, RepoArchive{
			Version:   v,
			IsLatest:  v == maxStable && maxStable != "",
			Artifacts: artifacts,
		})
	}
	return manifest, nil
}

// maxStableVersion returns the greatest pure X.Y.Z among the given version
// keys, ignoring any pre-release versions (e.g. 25.3.5-rc1).
func maxStableVersion(byVersion map[string][]info) string {
	var best string
	var bestTuple [3]int
	for v := range byVersion {
		m := stableVersionRe.FindStringSubmatch(v)
		if m == nil {
			continue
		}
		maj, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		tuple := [3]int{maj, minor, patch}
		if best == "" || slices.Compare(bestTuple[:], tuple[:]) < 0 {
			best = v
			bestTuple = tuple
		}
	}
	return best
}
