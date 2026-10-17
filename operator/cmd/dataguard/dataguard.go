// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package dataguard is the init container that refuses to boot Redpanda on
// an empty data directory when the pod carries a node_id (see the Broker
// API's BrokerIDAnnotation for why). Redpanda loads its node UUID from the
// kvstore; with none there it would register as a NEW node.
package dataguard

import (
	"fmt"
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
		brokerIDFile string
		dataDir      string
	)

	cmd := &cobra.Command{
		Use:   "data-guard",
		Short: "Refuse to start a broker whose data directory is empty while a node_id is on record",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reason, err := Check(brokerIDFile, dataDir)
			if err != nil || reason == "" {
				return err
			}
			logger := ctrl.LoggerFrom(cmd.Context())
			logger.Error(errors.New(reason), "refusing to start Redpanda")
			// The kubelet copies the termination log into the container's
			// terminated state, where it shows up in pod status.
			if werr := os.WriteFile("/dev/termination-log", []byte(reason), 0o644); werr != nil {
				logger.Error(werr, "could not write the termination message")
			}
			os.Exit(ExitCodeEmptyDataDir)
			return nil
		},
	}

	// The default is where the brokerset mounts the downward API projection
	// of the pod's broker-id annotation.
	cmd.Flags().StringVar(&brokerIDFile, "broker-id-file", "/etc/broker-id/broker-id", "File holding the pod's node_id (empty when the broker never registered).")
	cmd.Flags().StringVar(&dataDir, "data-dir", "/var/lib/redpanda/data", "Redpanda data directory.")

	return cmd
}

// Check returns the reason to refuse starting the broker — non-empty only
// when brokerIDFile names a node_id and dataDir holds no kvstore — or an
// error when the guard could not do its job.
func Check(brokerIDFile, dataDir string) (string, error) {
	raw, err := os.ReadFile(brokerIDFile)
	if err != nil {
		return "", errors.Wrap(err, "reading the broker id")
	}
	// An empty file is the downward API's rendering of a missing
	// annotation: this broker never registered, so a fresh data directory
	// is the expected state.
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", nil
	}

	// Redpanda persists the node UUID into the kvstore right after
	// generating it on first boot, so any segment under a shard directory
	// means an identity exists and will be reloaded. The pattern is fixed,
	// so Glob cannot fail.
	segments, _ := filepath.Glob(filepath.Join(dataDir, "redpanda", "kvstore", "*", "*"))
	if len(segments) > 0 {
		return "", nil
	}
	return fmt.Sprintf("data directory %s holds no kvstore but this pod is node_id %s: refusing to start, Redpanda would re-register under a new node_id", dataDir, id), nil
}
