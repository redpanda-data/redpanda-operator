// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package dataguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func TestCheck(t *testing.T) {
	kvstoreSegment := filepath.Join("redpanda", "kvstore", "0_0", "0-1-v1.log")
	for _, tc := range []struct {
		name     string
		brokerID *string // nil: no file at all
		files    []string
		dirs     []string
		// missing leaves the data directory out entirely, as in a pod whose
		// volume never mounted; notDir puts a plain file at its path.
		missing bool
		notDir  bool
		// unreadable chmods the volume to 0000; skipped as root, who reads
		// anything.
		unreadable bool
		want       int
		wantReason []string
	}{
		{name: "never registered, fresh disk", brokerID: ptr.To(""), want: ExitCodeAllow},
		{name: "registered, kvstore present", brokerID: ptr.To("3"), files: []string{kvstoreSegment}, want: ExitCodeAllow},
		{name: "registered, whitespace around the id", brokerID: ptr.To(" 3\n"), files: []string{kvstoreSegment}, want: ExitCodeAllow},
		{name: "registered, empty volume", brokerID: ptr.To("3"), want: ExitCodeEmptyDataDir, wantReason: []string{"node_id 3", "is empty"}},
		{name: "registered, volume holds only lost+found", brokerID: ptr.To("3"), dirs: []string{"lost+found"}, want: ExitCodeEmptyDataDir, wantReason: []string{"node_id 3", "is empty"}},
		// Anything the guard cannot prove is Undecided: nothing starts, nothing is deleted.
		{name: "registered, stray file but no redpanda/", brokerID: ptr.To("3"), files: []string{"pid.lock"}, want: ExitCodeUndecided, wantReason: []string{"no kvstore", "refusing to guess"}},
		{name: "registered, kvstore dir but no segments", brokerID: ptr.To("3"), dirs: []string{filepath.Join("redpanda", "kvstore", "0_0")}, want: ExitCodeUndecided, wantReason: []string{"no kvstore"}},
		{name: "registered, controller log but no kvstore", brokerID: ptr.To("3"), files: []string{filepath.Join("redpanda", "controller", "0_0", "0-1-v1.log")}, want: ExitCodeUndecided, wantReason: []string{"no kvstore"}},
		{name: "registered, data directory missing (volume not mounted)", brokerID: ptr.To("3"), missing: true, want: ExitCodeUndecided, wantReason: []string{"does not exist", "not mounted"}},
		{name: "registered, data path is a file", brokerID: ptr.To("3"), notDir: true, want: ExitCodeUndecided, wantReason: []string{"reading the data directory"}},
		{name: "registered, data directory unreadable", brokerID: ptr.To("3"), unreadable: true, want: ExitCodeUndecided, wantReason: []string{"is not readable"}},
		{name: "broker id file missing", brokerID: nil, files: []string{kvstoreSegment}, want: ExitCodeUndecided, wantReason: []string{"reading the broker id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unreadable && os.Geteuid() == 0 {
				t.Skip("root reads directories regardless of their mode")
			}
			root := t.TempDir()
			idFile := filepath.Join(root, "broker-id")
			if tc.brokerID != nil {
				require.NoError(t, os.WriteFile(idFile, []byte(*tc.brokerID), 0o644))
			}
			volume := filepath.Join(root, "data")
			switch {
			case tc.missing:
			case tc.notDir:
				require.NoError(t, os.WriteFile(volume, []byte("x"), 0o644))
			default:
				require.NoError(t, os.MkdirAll(volume, 0o755))
			}
			for _, d := range tc.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(volume, d), 0o755))
			}
			for _, f := range tc.files {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(volume, f)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(volume, f), []byte("x"), 0o644))
			}
			if tc.unreadable {
				require.NoError(t, os.Chmod(volume, 0o000))
				t.Cleanup(func() { _ = os.Chmod(volume, 0o755) })
			}

			verdict, reason := Check(logr.Discard(), idFile, volume)
			require.Equal(t, Verdict(tc.want), Verdict(verdict))
			if tc.want == ExitCodeAllow {
				require.Empty(t, reason)
				return
			}
			for _, want := range tc.wantReason {
				require.Contains(t, reason, want)
			}
		})
	}
}
