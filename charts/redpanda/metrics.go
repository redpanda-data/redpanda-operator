// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	MetricsEnvVarDeploymentType        = "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE"
	MetricsEnvVarChartVersion          = "REDPANDA_METRICS_K8S_CHART_VERSION"
	MetricsEnvVarOperatorVersion       = "REDPANDA_METRICS_K8S_OPERATOR_IMAGE_VERSION"
	MetricsEnvVarKubernetesVersion     = "REDPANDA_METRICS_K8S_VERSION"
	MetricsEnvVarKubernetesEnvironment = "REDPANDA_METRICS_K8S_ENVIRONMENT"
	MetricsEnvVarClusterID             = "REDPANDA_METRICS_K8S_CLUSTER_ID"

	deploymentTypeHelm     = "helm"
	deploymentTypeOperator = "operator"
)

// NewMetrics resolves the telemetry context reported by the Redpanda container.
// See [Metrics.EnvironmentVariables].
func NewMetrics(dot *helmette.Dot, values *Values) Metrics {
	// enable_metrics_reporter defaults to true, so unless someone has explicitly opted out
	// let's attempt to inject additional context into the metrics
	coreMetricsEnabled := helmette.Dig(values.Config.Cluster, true, "enable_metrics_reporter").(bool)
	if !coreMetricsEnabled {
		return Metrics{}
	}

	// secondarily let's attempt to check the usage stats flag that appears
	// to be used nowhere, this is mainly as a fail-safe so that
	// if various dynamic lookup components can't succeed (i.e. the UID)
	// determination below, we can still succeed in deploying the cluster
	// by disabling this flag on our values.
	if !values.Logging.UsageStats.Enabled {
		return Metrics{}
	}

	kubeVersion := dot.Capabilities.KubeVersion.Version

	metrics := Metrics{
		Enabled:           true,
		KubernetesVersion: kubeVersion,
		ChartVersion:      dot.Chart.Version,
	}

	// UID of the kube-system namespace to fingerprint the cluster: https://opentelemetry.io/docs/specs/semconv/resource/k8s/#cluster
	namespace, ok := helmette.Lookup[corev1.Namespace](dot, "", "kube-system")
	if ok {
		metrics.ClusterID = string(namespace.ObjectMeta.UID)
	}

	// TODO: do some more digging to see if we can detect Azure
	if strings.Contains(kubeVersion, "-gke") {
		metrics.CloudEnvironment = "GCP"
	} else if strings.Contains(kubeVersion, "-eks") {
		metrics.CloudEnvironment = "AWS"
	}

	return metrics
}

// Metrics is the telemetry context injected into the Redpanda container as
// REDPANDA_METRICS_K8S_* environment variables. See [NewMetrics].
type Metrics struct {
	// Enabled reports whether the user has opted in to metrics. When false
	// every other field is zero and [Metrics.EnvironmentVariables] returns
	// nothing; [NewMetrics] skips its cluster lookup entirely.
	Enabled bool

	// ViaOperator says that this chart is being rendered by the operator rather
	// than by helm.
	ViaOperator bool

	// CloudEnvironment is the cloud environment (Azure, GCP, AWS) this is
	// deployed in, determined from the Kubernetes version string.
	CloudEnvironment string

	// KubernetesVersion is the full Kubernetes server version string.
	KubernetesVersion string

	// ChartVersion is the version of the chart being rendered as reported by
	// helm OR the operator's version stamped at build time.
	ChartVersion string

	// ClusterID is the UID of the kube-system namespace, used as a cluster
	// fingerprint.
	ClusterID string
}

// EnvironmentVariables returns the REDPANDA_METRICS_K8S_* environment variables
// for pool's Redpanda container, or nil if the user has opted out of metrics.
func (m Metrics) EnvironmentVariables(pool Pool) []corev1.EnvVar {
	if !m.Enabled {
		return nil
	}

	deploymentType := deploymentTypeHelm
	if m.ViaOperator {
		deploymentType = deploymentTypeOperator
	}

	envvars := []corev1.EnvVar{{
		Name:  MetricsEnvVarKubernetesVersion,
		Value: m.KubernetesVersion,
	}, {
		Name:  MetricsEnvVarDeploymentType,
		Value: deploymentType,
	}, {
		Name:  MetricsEnvVarChartVersion,
		Value: m.ChartVersion,
	}, {
		Name:  MetricsEnvVarOperatorVersion,
		Value: fmt.Sprintf(`%s:%s`, pool.Statefulset.SideCars.Image.Repository, pool.Statefulset.SideCars.Image.Tag),
	}}

	if m.ClusterID != "" {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarClusterID,
			Value: m.ClusterID,
		})
	}

	if m.CloudEnvironment != "" {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarKubernetesEnvironment,
			Value: m.CloudEnvironment,
		})
	}

	return envvars
}
