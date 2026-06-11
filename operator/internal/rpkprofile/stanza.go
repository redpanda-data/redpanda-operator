// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package rpkprofile

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// SyncRPKStanza grafts the `rpk:` stanza from the chart-rendered source
// redpanda.yaml (the kubelet-refreshed ConfigMap mount, /tmp/base-config in
// v2 pods) into the pod-local destination redpanda.yaml, preserving every
// other key — in particular the per-pod mutations the v2 configurator init
// script applied at startup (advertised addresses, node ID, rack).
//
// The v2 chart embeds the in-pod rpk configuration (broker, admin, and
// schema-registry address lists) in that stanza, so this is the v2
// equivalent of re-rendering the v1 rpk profile file.
//
// A source without an `rpk` key is a no-op rather than an error: it must
// never clobber a working destination during a partially-propagated mount
// update. The destination is replaced atomically (temp file + rename) since
// in-pod rpk may read it concurrently.
func SyncRPKStanza(sourcePath, destPath string) error {
	srcBuf, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("reading source %q: %w", sourcePath, err)
	}
	var src map[string]any
	if err := yaml.Unmarshal(srcBuf, &src); err != nil {
		return fmt.Errorf("parsing source %q: %w", sourcePath, err)
	}
	rpkStanza, ok := src["rpk"]
	if !ok {
		return nil
	}

	dstBuf, err := os.ReadFile(destPath)
	if err != nil {
		return fmt.Errorf("reading destination %q: %w", destPath, err)
	}
	var dst map[string]any
	if err := yaml.Unmarshal(dstBuf, &dst); err != nil {
		return fmt.Errorf("parsing destination %q: %w", destPath, err)
	}
	if dst == nil {
		dst = map[string]any{}
	}
	dst["rpk"] = rpkStanza

	out, err := yaml.Marshal(dst)
	if err != nil {
		return fmt.Errorf("serializing destination: %w", err)
	}

	return WriteFileAtomic(destPath, out, 0o644)
}

// WriteFileAtomic replaces path with data atomically: it writes a temp file
// in the same directory, then renames it over the destination. Renders
// happen while in-pod rpk may be reading the file concurrently, so a plain
// os.WriteFile (truncate then write) could expose a partial file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	return os.Rename(tmpName, path)
}
