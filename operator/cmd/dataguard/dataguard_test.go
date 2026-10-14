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
		name      string
		brokerID  *string // nil: no file at all
		files     []string
		dirs      []string
		wantEmpty bool
		wantErr   bool
	}{
		{name: "never registered, fresh disk", brokerID: ptr.To(""), wantEmpty: false},
		{name: "registered, kvstore present", brokerID: ptr.To("3"), files: []string{kvstoreSegment}},
		{name: "registered, whitespace around the id", brokerID: ptr.To(" 3\n"), files: []string{kvstoreSegment}},
		{name: "registered, data dir missing", brokerID: ptr.To("3"), wantEmpty: true},
		{name: "registered, data dir without redpanda/", brokerID: ptr.To("3"), files: []string{"pid.lock"}, wantEmpty: true},
		{name: "registered, kvstore dir but no segments", brokerID: ptr.To("3"), dirs: []string{filepath.Join("redpanda", "kvstore", "0_0")}, wantEmpty: true},
		{name: "registered, controller log but no kvstore", brokerID: ptr.To("3"), files: []string{filepath.Join("redpanda", "controller", "0_0", "0-1-v1.log")}, wantEmpty: true},
		{name: "broker id file missing", brokerID: nil, files: []string{kvstoreSegment}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			idFile := filepath.Join(root, "broker-id")
			if tc.brokerID != nil {
				require.NoError(t, os.WriteFile(idFile, []byte(*tc.brokerID), 0o644))
			}
			dataDir := filepath.Join(root, "data")
			for _, d := range tc.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(dataDir, d), 0o755))
			}
			for _, f := range tc.files {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dataDir, f)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dataDir, f), []byte("x"), 0o644))
			}

			reason, err := Check(logr.Discard(), idFile, dataDir)
			switch {
			case tc.wantErr:
				require.Error(t, err)
				require.Empty(t, reason, "a guard failure is not a verdict")
			case tc.wantEmpty:
				require.NoError(t, err)
				require.Contains(t, reason, "node_id 3")
				require.Contains(t, reason, dataDir)
			default:
				require.NoError(t, err)
				require.Empty(t, reason)
			}
		})
	}
}
