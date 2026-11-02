// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package detectdisklost is the init container that refuses to boot Redpanda on
// an empty data directory when the pod carries a node_id (see the Broker
// API's BrokerIDAnnotation for why). Redpanda loads its node UUID from the
// kvstore; with none there it would register as a NEW node.
package detectdisklost

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Exit statuses of the guard.
const (
	ExitCodeAllow = 0
	// ExitCodeEmptyDataDir means a node_id is on record and the data directory
	// is provably empty. The Broker controller treats it as disk loss and
	// dismantles the Broker — the one verdict that ends in a deleted PVC, so
	// it is the only non-zero status the guard returns, distinct from 1 so a
	// crash of the guard itself (a cobra error, a panic) is never mistaken
	// for it. Everything the guard cannot prove is left to Redpanda, which
	// fails on a broken volume with its own errors.
	ExitCodeEmptyDataDir = 3
)

func Verdict(exitCode int) string {
	switch exitCode {
	case ExitCodeAllow:
		return "allow"
	case ExitCodeEmptyDataDir:
		return "empty"
	default:
		return "unknown"
	}
}

func Command() *cobra.Command {
	var (
		brokerIDFile string
		dataDir      string
	)

	cmd := &cobra.Command{
		Use:   "detect-disk-lost",
		Short: "Refuse to start a broker whose data directory is empty while a node_id is on record",
		Run: func(cmd *cobra.Command, _ []string) {
			logger := ctrl.LoggerFrom(cmd.Context())
			exitCode, reason := Check(logger, brokerIDFile, dataDir)
			if exitCode == ExitCodeAllow {
				return
			}
			logger.Error(nil, "refusing to start Redpanda", "verdict", Verdict(exitCode), "reason", reason)
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

// Check decides whether Redpanda may start. ExitCodeEmptyDataDir is returned
// only for a data directory that is provably empty — readable and holding
// nothing but lost+found — while a node_id is on record: that verdict ends
// in the Broker's PVC being deleted. Anything the guard cannot prove — an
// unreadable broker id, a missing or unreadable directory, data without a
// kvstore — is left to Redpanda, which fails on a broken volume with its own
// errors and never discards an identity it can still read. The reason is
// empty for ExitCodeAllow.
func Check(logger logr.Logger, brokerIDFile, dataDir string) (int, string) {
	raw, err := os.ReadFile(brokerIDFile)
	if err != nil {
		logger.Info("cannot read the broker id, leaving the decision to Redpanda", "filepath", brokerIDFile, "error", err.Error())
		return ExitCodeAllow, ""
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

	// The guard runs in the distroless operator image, which has no
	// /var/lib/redpanda/data of its own, so a missing directory means the
	// volume is not mounted there; an unreadable one is Redpanda's problem
	// to report.
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		logger.Info("cannot read the data directory, leaving the decision to Redpanda", "dataDir", dataDir, "error", err.Error())
		return ExitCodeAllow, ""
	}
	// A freshly provisioned filesystem carries lost+found and nothing else.
	containsAnything := slices.ContainsFunc(entries, func(e os.DirEntry) bool { return e.Name() != "lost+found" })
	if !containsAnything {
		return ExitCodeEmptyDataDir, fmt.Sprintf("data directory %s is empty but this pod is node_id %s: refusing to start, Redpanda would re-register under a new node_id", dataDir, id)
	}

	// Redpanda persists the node UUID into the kvstore right after
	// generating it on first boot, so any segment under a shard directory
	// means an identity exists and will be reloaded. The pattern is fixed,
	// so Glob cannot fail.
	segments, _ := filepath.Glob(filepath.Join(dataDir, "redpanda", "kvstore", "*", "*"))
	if len(segments) == 0 {
		logger.Info("data directory holds data but no kvstore, leaving the decision to Redpanda", "dataDir", dataDir, "node_id", id)
		return ExitCodeAllow, ""
	}
	logger.Info("kvstore present, the recorded identity will be reloaded", "node_id", id, "dataDir", dataDir)
	return ExitCodeAllow, ""
}
