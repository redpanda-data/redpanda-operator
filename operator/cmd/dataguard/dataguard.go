// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package dataguard is the init container that keeps a Broker pod from
// booting Redpanda on an empty data directory when the pod already has a
// node_id on record. Redpanda loads its node UUID from shard 0's kvstore; an
// empty kvstore means it mints a new UUID and the cluster assigns a NEW
// node_id, orphaning the recorded one as a permanently-down member. Failing
// here turns that silent identity change into a stuck pod the Broker
// controller can act on.
package dataguard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ExitCodeEmptyDataDir is the exit status for "node_id on record but no
// kvstore on disk". Distinct from 1, so a crash of the guard itself is never
// mistaken for its verdict.
const ExitCodeEmptyDataDir = 3

func Command() *cobra.Command {
	var (
		brokerIDFile   string
		dataDir        string
		terminationLog string
	)

	cmd := &cobra.Command{
		Use:   "data-guard",
		Short: "Refuse to start a broker whose data directory is empty while a node_id is on record",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := ctrl.LoggerFrom(cmd.Context())

			err := Check(brokerIDFile, dataDir)
			var empty *EmptyDataDirError
			if !errors.As(err, &empty) {
				return err
			}
			logger.Error(err, "refusing to start Redpanda", "brokerID", empty.BrokerID, "dataDir", empty.DataDir)
			// The kubelet copies this file into the container's terminated
			// state, where the Broker controller reads it from pod status.
			if werr := os.WriteFile(terminationLog, []byte(err.Error()), 0o644); werr != nil {
				logger.Error(werr, "could not write the termination message", "path", terminationLog)
			}
			os.Exit(ExitCodeEmptyDataDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&brokerIDFile, "broker-id-file", "", "File holding the pod's node_id (empty when the broker never registered).")
	cmd.Flags().StringVar(&dataDir, "data-dir", "/var/lib/redpanda/data", "Redpanda data directory.")
	cmd.Flags().StringVar(&terminationLog, "termination-log", "/dev/termination-log", "Where to write the reason when refusing to start.")

	return cmd
}

// EmptyDataDirError is the guard's verdict: an identity is on record, the
// data directory has none.
type EmptyDataDirError struct {
	BrokerID string
	DataDir  string
}

func (e *EmptyDataDirError) Error() string {
	return fmt.Sprintf("data directory %s holds no kvstore but this pod is node_id %s: refusing to start, Redpanda would re-register under a new node_id", e.DataDir, e.BrokerID)
}

// Check returns an *EmptyDataDirError when brokerIDFile names a node_id and
// dataDir holds no kvstore, nil when the broker may start, and any other
// error for a guard that could not do its job.
func Check(brokerIDFile, dataDir string) error {
	raw, err := os.ReadFile(brokerIDFile)
	if err != nil {
		return errors.Wrap(err, "reading the broker id")
	}
	// An empty file is the downward API's rendering of a missing
	// annotation: this broker never registered, so a fresh data directory
	// is the expected state.
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return nil
	}

	present, err := kvstorePresent(dataDir)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	return &EmptyDataDirError{BrokerID: id, DataDir: dataDir}
}

// kvstorePresent reports whether the kvstore has any segment on disk.
// Redpanda persists the node UUID there right after generating it on first
// boot, so a segment means an identity exists and will be reloaded. An
// absent or empty kvstore directory counts as no identity.
func kvstorePresent(dataDir string) (bool, error) {
	root := filepath.Join(dataDir, "redpanda", "kvstore")
	var found bool
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "inspecting %s", root)
	}
	return found, nil
}
