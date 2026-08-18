// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestForkLedger is the drift guard for this package's one-time
// de-genericization fork of the OSS lifecycle framework. Each entry pins the
// sha256 of the CURRENT content of an OSS ancestor file that a file in this
// package (or in enterprise/pkg/multicluster) was concretized from. When an
// ancestor changes, this test fails: review the OSS change, port it into the
// enterprise concretization if it applies (or record the intentional
// divergence in the enterprise copy), then update the pinned hash here.
//
// This test only works while the enterprise module still lives inside the
// redpanda-operator monorepo; it skips when an ancestor path does not exist
// (the post-lift state). Per the carve-out runbook, delete this test at lift
// time.
func TestForkLedger(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolving caller path")
	}
	// The monorepo root is three levels up from enterprise/operator/lifecycle.
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")

	// Map of OSS ancestor file (relative to the monorepo root) -> sha256 of
	// the content this package's concretization was last reconciled against.
	ancestors := map[string]string{
		// De-genericized into this package's files of the same name.
		"operator/internal/lifecycle/client.go":     "061b210e18a8be2fb9d52fd0f3d18859eb0844a63bde4a3019777f7ccdf6ddda",
		"operator/internal/lifecycle/pool.go":       "2a2e20f868f2d6d0a726914a0aa282e0b4a7a832e1415d5f88a3874d1c43b705",
		"operator/internal/lifecycle/interfaces.go": "d275d1ade97682ad862bc6dbeda02ec22543f7ab41e3cad7400526a6be4a1c70",
		"operator/internal/lifecycle/helpers.go":    "1c84b970eedf8321809c4c272f7d386361931c351d09c5d013bf5131f6952d8b",
		"operator/internal/lifecycle/secrets.go":    "3abff4936c961a0e9edecd5e950a8de985f1c96a25d444139d971f0faf39d328",
		"operator/internal/lifecycle/constants.go":  "d10c5266ad6e6456550f46bf69bcaae2133b844956cdb9788ed190f9fafaf70e",
		// Copied into this package's tolerations.go.
		"operator/pkg/resources/tolerations.go": "4da432239d905f7fb836add3d28839735746ec26d41e07e0fd54124fbb89da0d",
		// WatchOptions copied into this package's watches.go.
		"operator/internal/controller/watches.go": "66cda71f16592f9d3648b133fcd343fc4b60c992a284dbf5d9e0e842f4ae5b08",
		// Copied into enterprise/pkg/multicluster/singlecluster.go (test-only
		// single-cluster manager used by this package's golden tests).
		"pkg/multicluster/singlecluster.go": "2e00f7254044b603b0b8ae7b3277fbb26d59f199d8fa466b0a4cb5f0cd4ac773",
	}

	for rel, want := range ancestors {
		t.Run(rel, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(rel))
			data, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				t.Skipf("OSS ancestor %s does not exist — assuming the enterprise module has been lifted out of the monorepo; this fork-ledger test should be deleted at lift time", rel)
			}
			if err != nil {
				t.Fatalf("reading OSS ancestor %s: %v", rel, err)
			}
			sum := sha256.Sum256(data)
			got := hex.EncodeToString(sum[:])
			if got != want {
				t.Errorf("OSS ancestor %s changed since the enterprise concretization was last reconciled with it.\n"+
					"  pinned sha256: %s\n"+
					"  current sha256: %s\n"+
					"Review the OSS change and port it into the corresponding file in enterprise/operator/lifecycle "+
					"if it applies (or record the intentional divergence there), then update the pinned hash in forkledger_test.go.",
					rel, want, got)
			}
		})
	}
}
