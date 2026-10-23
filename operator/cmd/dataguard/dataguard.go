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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Exit statuses of the guard. The non-zero ones are distinct from 1, so a
// crash of the guard itself (a cobra error, a panic) is never mistaken for a
// verdict.
const (
	ExitCodeAllow = 0
	// ExitCodeEmptyDataDir means a node_id is on record and the data directory is
	// provably empty. The Broker controller treats it as disk loss and
	// dismantles the Broker — the one verdict that ends in a deleted PVC.
	ExitCodeEmptyDataDir = 3
	// ExitCodeUndecided means a node_id is on record but the guard could not
	// prove the directory empty (volume not mounted, unreadable, data
	// present without a kvstore). Redpanda must not start, and nothing may
	// be deleted either: the pod stays in CrashLoopBackOff for a human.
	ExitCodeUndecided = 4
)

func Verdict(exitCode int) string {
	switch exitCode {
	case ExitCodeAllow:
		return "allow"
	case ExitCodeEmptyDataDir:
		return "empty"
	default:
		return "undecided"
	}
}

func Command() *cobra.Command {
	var (
		brokerIDFile string
		dataDir      string
	)

	cmd := &cobra.Command{
		Use:   "data-guard",
		Short: "Refuse to start a broker whose data directory is empty while a node_id is on record",
		Run: func(cmd *cobra.Command, _ []string) {
			logger := ctrl.LoggerFrom(cmd.Context())
			exitCode, reason := Check(logger, brokerIDFile, dataDir)
			if exitCode == ExitCodeAllow {
				return
			}
			logger.Error(errors.New(reason), "refusing to start Redpanda", "verdict", Verdict(exitCode))
			// The kubelet copies the termination log into the container's
			// terminated state, where it shows up in pod status.
			if werr := os.WriteFile("/dev/termination-log", []byte(reason), 0o644); werr != nil {
				logger.Error(werr, "could not write the termination message")
			}
			os.Exit(exitCode)
		},
	}

	// The default is where the brokerset mounts the downward API projection
	// of the pod's broker-id annotation.
	cmd.Flags().StringVar(&brokerIDFile, "broker-id-file", "/etc/broker-id/broker-id", "File holding the pod's node_id (empty when the broker never registered).")
	cmd.Flags().StringVar(&dataDir, "data-dir", "/var/lib/redpanda/data", "Redpanda data directory, the mount point of the data volume.")

	return cmd
}

// Check decides whether Redpanda may start. Empty is returned only for a
// data directory that is provably empty — readable and holding nothing but
// lost+found — while a node_id is on record: that verdict ends in the
// Broker's PVC being deleted, so a missing mount, a permission error or a
// layout the guard does not understand must never produce it. Every such
// doubt is Undecided, which keeps the pod from starting without destroying
// anything. The reason is empty for Allow.
func Check(logger logr.Logger, brokerIDFile, dataDir string) (int, string) {
	raw, err := os.ReadFile(brokerIDFile)
	if err != nil {
		return ExitCodeUndecided, fmt.Sprintf("reading the broker id from %s: %v", brokerIDFile, err)
	}
	// An empty file is the downward API's rendering of a missing
	// annotation: this broker never registered, so a fresh data directory
	// is the expected state.
	id := strings.TrimSpace(string(raw))
	if id == "" {
		logger.Info("no node_id on record, a fresh data directory is expected")
		return ExitCodeAllow, ""
	}
	logger.Info("node_id on record", "node_id", id)

	entries, err := os.ReadDir(dataDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The guard runs in the distroless operator image, which has no
		// /var/lib/redpanda/data of its own: the path exists only when the
		// data volume is mounted there.
		return ExitCodeUndecided, fmt.Sprintf("data directory %s does not exist: the data volume is not mounted", dataDir)
	case errors.Is(err, fs.ErrPermission):
		return ExitCodeUndecided, fmt.Sprintf("data directory %s is not readable: %v", dataDir, err)
	case err != nil:
		return ExitCodeUndecided, fmt.Sprintf("reading the data directory %s: %v", dataDir, err)
	}
	// A freshly provisioned filesystem carries lost+found and nothing else.
	containsAnything := slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() != "lost+found" })
	if !containsAnything {
		return ExitCodeEmptyDataDir, fmt.Sprintf("data directory %s is empty but this pod is node_id %s: refusing to start, Redpanda would re-register under a new node_id", dataDir, id)
	}

	// Redpanda persists the node UUID into the kvstore right after
	// generating it on first boot, so any segment under a shard directory
	// means an identity exists and will be reloaded. Glob only fails on a
	// malformed pattern and skips directories it cannot read, so an
	// unreadable kvstore lands in the Undecided fall-through below.
	segments, err := filepath.Glob(filepath.Join(dataDir, "redpanda", "kvstore", "*", "*"))
	if err != nil {
		return ExitCodeUndecided, fmt.Sprintf("looking for the kvstore under %s: %v", dataDir, err)
	}
	if len(segments) > 0 {
		logger.Info("kvstore present, the recorded identity will be reloaded", "node_id", id, "dataDir", dataDir)
		return ExitCodeAllow, ""
	}
	return ExitCodeUndecided, fmt.Sprintf("data directory %s holds data but no kvstore while this pod is node_id %s: refusing to guess", dataDir, id)
}
