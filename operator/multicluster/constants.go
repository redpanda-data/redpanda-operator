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
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// defaultAPITokenMountPath is the default mount path for Kubernetes API tokens.
	//nolint:gosec
	defaultAPITokenMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

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
	// serviceAccountVolumeName is the name of the projected service account token volume.
	serviceAccountVolumeName = "kube-api-access"

	// lifecycleScriptsVolumeName is the volume name for lifecycle hook scripts.
	lifecycleScriptsVolumeName = "lifecycle-scripts"

	// baseConfigVolumeName is the volume name for the base ConfigMap.
	baseConfigVolumeName = "base-config"

	// configVolumeName is the volume name for the generated Redpanda config.
	configVolumeName = "config"

	// datadirVolumeName is the volume and PVC name for the Redpanda data directory.
	datadirVolumeName = "datadir"

	// tokenExpirationSeconds is the expiration time for projected service account tokens.
	tokenExpirationSeconds = 60*60 + 7
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

	// nodePoolLabelName is the label key for identifying which node pool a pod belongs to.
	nodePoolLabelName = "cluster.redpanda.com/nodepool-name"
	// nodePoolLabelGeneration is the label key for tracking node pool generation.
	nodePoolLabelGeneration = "cluster.redpanda.com/nodepool-generation"
)

// Well-known label values.
const (
	labelNameValue      = "redpanda"
	labelManagedByValue = "redpanda-operator"
)

// Container names.
const (
	redpandaContainerName                  = "redpanda"
	sidecarContainerName                   = "sidecar"
	redpandaConfiguratorContainerName      = "redpanda-configurator"
	redpandaTuningContainerName            = "tuning"
	setDataDirectoryOwnershipContainerName = "set-datadir-ownership"
	fsValidatorContainerName               = "fs-validator"
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

// Field owner for server-side apply.
var defaultFieldOwner = client.FieldOwner("cluster.redpanda.com/operator")

// defaultBootstrapUsername is the default SCRAM username created by
// RP_BOOTSTRAP_USER and added to the superusers list.
const defaultBootstrapUsername = "kubernetes-controller"

// Supported SASL mechanisms.
var supportedSASLMechanisms = []string{
	"SCRAM-SHA-256", "SCRAM-SHA-512",
}

// SASL error sentinels.
var (
	errSASLSecretNotFound          = fmt.Errorf("users secret not found")
	errSASLSecretKeyNotFound       = fmt.Errorf("users secret key not found")
	errSASLSecretSuperuserNotFound = fmt.Errorf("users secret has no users")
)

// TLS error sentinels.
var (
	errServerCertificateNotFound          = errors.New("server TLS certificate not found")
	errServerCertificatePublicKeyNotFound = errors.New("server TLS certificate does not contain a public key")

	errClientCertificateNotFound           = errors.New("client TLS certificate not found")
	errClientCertificatePublicKeyNotFound  = errors.New("client TLS certificate does not contain a public key")
	errClientCertificatePrivateKeyNotFound = errors.New("client TLS certificate does not contain a private key")
)
