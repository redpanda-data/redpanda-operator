// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"time"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// Rendering-specific defaults (not inherent to the CRD types).
const (
	// defaultCertDuration is the default duration for cert-manager certificates (5 years).
	defaultCertDuration = 43800 * time.Hour

	// defaultTerminationGracePeriod is the default termination grace period in seconds.
	defaultTerminationGracePeriod = int64(90)

	// redpandaUserID and redpandaGroupID are the UID/GID for the redpanda user.
	redpandaUserID  = int64(101)
	redpandaGroupID = int64(101)

	// sidecarHealthPort is the port for the sidecar readiness probe.
	sidecarHealthPort = int32(8093)
	// sidecarHealthPath is the path for the sidecar readiness probe.
	sidecarHealthPath = "/healthz"

	// publicMetricsPath is the path for the Prometheus metrics endpoint.
	publicMetricsPath = "/public_metrics"
)

// Mount paths.
const (
	// redpandaConfigMountPath is the path where the final Redpanda config is mounted.
	redpandaConfigMountPath = "/etc/redpanda"

	// baseConfigMountPath is the path where the base ConfigMap config is mounted.
	baseConfigMountPath = "/tmp/base-config"

	// lifecycleScriptsMountPath is the path where lifecycle hook scripts are mounted.
	lifecycleScriptsMountPath = "/var/lifecycle"

	// datadirMountPath is the path where the Redpanda data directory is mounted.
	datadirMountPath = "/var/lib/redpanda/data"
)

// Volume names.
const (
	// lifecycleScriptsVolumeName is the volume name for lifecycle hook scripts.
	lifecycleScriptsVolumeName = "lifecycle-scripts"

	// baseConfigVolumeName is the volume name for the base ConfigMap.
	baseConfigVolumeName = "base-config"

	// configVolumeName is the volume name for the generated Redpanda config.
	configVolumeName = "config"

	// datadirVolumeName is the volume and PVC name for the Redpanda data directory.
	datadirVolumeName = "datadir"
)

// Well-known Kubernetes label keys.
const (
	labelNameKey        = "app.kubernetes.io/name"
	labelInstanceKey    = "app.kubernetes.io/instance"
	labelComponentKey   = "app.kubernetes.io/component"
	labelManagedByKey   = "app.kubernetes.io/managed-by"
	labelClusterNameKey = "app.kubernetes.io/cluster-name"

	labelPDBKey     = "redpanda.com/poddisruptionbudget"
	labelBrokerKey  = "cluster.redpanda.com/broker"
	labelMonitorKey = "monitoring.redpanda.com/enabled"

	// BrokerPoolLabelName is the label key for identifying which broker pool a pod belongs to.
	BrokerPoolLabelName = "cluster.redpanda.com/brokerpool-name"
	// BrokerPoolLabelGeneration is the label key for tracking broker pool generation.
	BrokerPoolLabelGeneration = "cluster.redpanda.com/brokerpool-generation"
)

// Well-known label values.
const (
	labelNameValue      = "redpanda"
	labelManagedByValue = "redpanda-operator"
)

// Internal service port names.
const (
	internalAdminAPIPortName       = "admin"
	internalKafkaPortName          = "kafka"
	internalSchemaRegistryPortName = "schemaregistry"
	internalPandaProxyPortName     = "http"
	internalRPCPortName            = "rpc"
)

// Internal listener name used in Redpanda configuration.
const internalListenerName = "internal"

// defaultBootstrapUsername is the default SCRAM username created by
// RP_BOOTSTRAP_USER and added to the superusers list. Aliases the canonical
// constant in v1alpha2 so callers outside this package don't have to depend
// on the API package just for the username string.
const defaultBootstrapUsername = redpandav1alpha2.StretchClusterBootstrapUsername
