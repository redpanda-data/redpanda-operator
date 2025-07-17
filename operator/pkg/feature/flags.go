package feature

import (
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/redpanda-data/redpanda-operator/operator/cmd/syncclusterconfig"
)

const (
	v2Prefix = "cluster.redpanda.com"
	v1Prefix = "redpanda.vectorized.io"
)

// V1Managed controls whether a Cluster resource is
// reconciled or by the cluster controller(s) or not.
// Valid Value(s): false
var V1Managed = Register(V1Flags, AnnotationFeatureFlag[bool]{
	// redpanda.vectorized.io/managed
	Key:     v2Prefix + "/managed",
	Default: "true",
	Parse: func(s string) (bool, error) {
		return s != "false", nil
	},
})

var (
	// V2Managed controls whether a Redpanda resource is
	// reconciled or by the redpanda controller(s) or not.
	// Valid Value(s): false
	V2Managed = Register(V2Flags, AnnotationFeatureFlag[bool]{
		// cluster.redpanda.com/managed
		Key:     v2Prefix + "/managed",
		Default: "true",
		Parse: func(s string) (bool, error) {
			return s != "false", nil
		},
	})

	// RestartOnConfigChange controls whether or not the Redpanda controller
	// will restart a cluster by injecting its cluster config version into its
	// PodSpec.
	// Valid Value(s): true
	RestartOnConfigChange = Register(V2Flags, AnnotationFeatureFlag[bool]{
		Key:     "operator.redpanda.com/restart-cluster-on-config-change",
		Default: "false",
		Parse: func(s string) (bool, error) {
			return s == "true", nil
		},
	})

	// ClusterConfigSyncMode controls how the Redpanda controller
	// synchronizes the cluster's cluster config.
	// Valid Value(s):
	// - additive: Set all keys, don't unset keys not explicit set
	// - declarative: Set all keys, unset any keys not explicitly set
	ClusterConfigSyncMode = Register(V2Flags, AnnotationFeatureFlag[syncclusterconfig.SyncerMode]{
		Key:     "operator.redpanda.com/config-sync-mode",
		Default: "additive",
		Parse: func(s string) (syncclusterconfig.SyncerMode, error) {
			switch strings.ToLower(s) {
			case "declarative":
				return syncclusterconfig.SyncerModeDeclarative, nil
			case "additive":
				return syncclusterconfig.SyncerModeAdditive, nil
			default:
				return syncclusterconfig.SyncerMode(0), errors.Newf("unknown cluster config syncer mode: %q", s)
			}
		},
	})
)
