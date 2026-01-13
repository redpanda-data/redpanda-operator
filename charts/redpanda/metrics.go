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

func MetricsEnvironmentVariables(state *RenderState, pool Pool) []corev1.EnvVar {
	// enable_metrics_reporter defaults to true, so unless someone has explicitly opted out
	// let's attempt to inject additional context into the metrics
	coreMetricsEnabled := helmette.Dig(state.Values.Config.Cluster, true, "enable_metrics_reporter").(bool)
	if !coreMetricsEnabled {
		return nil
	}

	// secondarily let's attempt to check the usage stats flag that appears
	// to be used nowhere, this is mainly as a fail-safe so that
	// if various dynamic lookup components can't succeed (i.e. the UID)
	// determination below, we can still succeed in deploying the cluster
	// by disabling this flag on our values.
	if !state.Values.Logging.UsageStats.Enabled {
		return nil
	}

	deploymentType := deploymentTypeHelm
	if state.ViaOperator {
		deploymentType = deploymentTypeOperator
	}
	envvars := []corev1.EnvVar{{
		Name:  MetricsEnvVarKubernetesVersion,
		Value: state.Dot.Capabilities.KubeVersion.Version,
	}, {
		Name:  MetricsEnvVarDeploymentType,
		Value: deploymentType,
	}, {
		Name:  MetricsEnvVarChartVersion,
		Value: state.Dot.Chart.Version,
	}, {
		Name:  MetricsEnvVarOperatorVersion,
		Value: fmt.Sprintf(`%s:%s`, pool.Statefulset.SideCars.Image.Repository, pool.Statefulset.SideCars.Image.Tag),
	}}

	// UID of the kube-system namespace to fingerprint the cluster: https://opentelemetry.io/docs/specs/semconv/resource/k8s/#cluster
	namespace, ok := helmette.Lookup[corev1.Namespace](state.Dot, "", "kube-system")
	if ok {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarClusterID,
			Value: string(namespace.ObjectMeta.UID),
		})
	}

	if !helmette.Empty(state.CloudEnvironment) {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarKubernetesEnvironment,
			Value: state.CloudEnvironment,
		})
	} else {
		// TODO: do some more digging to see if we can detect Azure
		if strings.Contains(state.Dot.Capabilities.KubeVersion.Version, "-gke") {
			envvars = append(envvars, corev1.EnvVar{
				Name:  MetricsEnvVarKubernetesEnvironment,
				Value: "GCP",
			})
		} else if strings.Contains(state.Dot.Capabilities.KubeVersion.Version, "-eks") {
			envvars = append(envvars, corev1.EnvVar{
				Name:  MetricsEnvVarKubernetesEnvironment,
				Value: "AWS",
			})
		}
	}

	return envvars
}
