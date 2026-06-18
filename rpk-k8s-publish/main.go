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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// defaultPlatforms mirrors the PLATFORMS default of `task ci:build:rpk-k8s`.
// The publisher requires every one of these before it will publish a version,
// so a partial cross-compile never yields a partially published plugin.
const defaultPlatforms = "linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

func main() {
	var (
		binaryDir    = flag.String("binary-dir", ".build", "Directory containing rpk-k8s-<goos>-<goarch> binaries")
		plugin       = flag.String("plugin", "k8s", "Managed plugin slug (S3 path + manifest path)")
		version      = flag.String("version", "", "Plugin version, e.g. 25.3.5 (a leading v is stripped)")
		bucket       = flag.String("bucket", "rpk-plugins-repo", "S3 bucket")
		region       = flag.String("region", "us-west-2", "S3 bucket region")
		repoHostname = flag.String("repo-hostname", "rpk-plugins.redpanda.com", "Public hostname serving the bucket")
		platforms    = flag.String("platforms", defaultPlatforms, "Required <goos>/<goarch> platforms (space- or comma-separated); all must be present or the publish fails")
		dryRun       = flag.Bool("dry-run", false, "Read from S3 but never write")
	)
	flag.Parse()

	if err := run(*binaryDir, *plugin, *version, *bucket, *region, *repoHostname, *platforms, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "rpk-k8s-publish: %v\n", err)
		os.Exit(1)
	}
}

// parsePlatforms splits a space- or comma-separated "<goos>/<goarch>" list
// into the "<goos>-<goarch>" keys used in archive paths and object tags.
func parsePlatforms(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, strings.ReplaceAll(f, "/", "-"))
	}
	return out
}

func run(binaryDir, plugin, version, bucket, region, repoHostname, platforms string, dryRun bool) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return fmt.Errorf("--version is required")
	}
	expectedPlatforms := parsePlatforms(platforms)
	if len(expectedPlatforms) == 0 {
		return fmt.Errorf("--platforms must list at least one <goos>/<goarch>")
	}
	binaryName := "redpanda-" + plugin

	ctx := context.Background()
	s3store, err := newS3Store(ctx, region, bucket)
	if err != nil {
		return err
	}
	var store objectStore = s3store
	if dryRun {
		store = &dryRunStore{inner: s3store}
	}

	if err := uploadArchives(ctx, store, binaryDir, plugin, binaryName, version, expectedPlatforms); err != nil {
		return err
	}

	manifest, err := buildManifest(ctx, store, plugin, binaryName, repoHostname)
	if err != nil {
		return err
	}
	manifest.CreatedAt = time.Now().Unix()

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestKey := plugin + "/manifest.json"
	fmt.Printf("writing manifest %s:\n%s\n", manifestKey, body)
	if err := store.put(ctx, manifestKey, body, nil); err != nil {
		return fmt.Errorf("unable to upload manifest: %w", err)
	}
	return nil
}

// dryRunStore wraps a real store: reads pass through, writes are logged and
// dropped. The dry-run archives are kept in-memory so the manifest rebuild
// reflects what *would* be published this run.
type dryRunStore struct {
	inner objectStore
	local map[string]map[string]string // key -> tags, for this run's would-be uploads
}

func (d *dryRunStore) put(_ context.Context, key string, body []byte, tags map[string]string) error {
	if d.local == nil {
		d.local = map[string]map[string]string{}
	}
	d.local[key] = tags
	fmt.Printf("DRY-RUN would upload %s (%d bytes)\n", key, len(body))
	return nil
}

func (d *dryRunStore) list(ctx context.Context, prefix string) ([]string, error) {
	keys, err := d.inner.list(ctx, prefix)
	if err != nil {
		return nil, err
	}
	for k := range d.local {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (d *dryRunStore) head(ctx context.Context, key string) (map[string]string, bool, error) {
	if t, ok := d.local[key]; ok {
		return t, true, nil
	}
	return d.inner.head(ctx, key)
}
