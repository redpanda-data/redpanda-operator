// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	MetricsEnvVarDeploymentType        = "REDPANDA_METRICS_K8S_DEPLOYMENT_TYPE"
	MetricsEnvVarChartVersion          = "REDPANDA_METRICS_K8S_CHART_VERSION"
	MetricsEnvVarConsoleImageVersion   = "REDPANDA_METRICS_K8S_CONSOLE_IMAGE_VERSION"
	MetricsEnvVarKubernetesVersion     = "REDPANDA_METRICS_K8S_VERSION"
	MetricsEnvVarKubernetesEnvironment = "REDPANDA_METRICS_K8S_ENVIRONMENT"
	MetricsEnvVarClusterID             = "REDPANDA_METRICS_K8S_CLUSTER_ID"

	deploymentTypeHelm     = "helm"
	deploymentTypeOperator = "operator"
)

func MetricsEnvironmentVariables(state *RenderState) []corev1.EnvVar {
	analyticsEnabled := helmette.Dig(state.Values.Config, true, "analytics", "enabled").(bool)
	if !analyticsEnabled {
		return nil
	}

	deploymentType := deploymentTypeHelm
	if state.Metrics.ViaOperator {
		deploymentType = deploymentTypeOperator
	}

	envvars := []corev1.EnvVar{{
		Name:  MetricsEnvVarDeploymentType,
		Value: deploymentType,
	}, {
		Name:  MetricsEnvVarChartVersion,
		Value: state.Metrics.ChartVersion,
	}, {
		Name:  MetricsEnvVarConsoleImageVersion,
		Value: fmt.Sprintf(`%s:%s`, state.Values.Image.Repository, consoleImageTag(state)),
	}}

	if state.Metrics.KubernetesVersion != "" {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarKubernetesVersion,
			Value: state.Metrics.KubernetesVersion,
		})
	}

	if state.Metrics.ClusterID != "" {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarClusterID,
			Value: state.Metrics.ClusterID,
		})
	}

	if state.Metrics.CloudEnvironment != "" {
		envvars = append(envvars, corev1.EnvVar{
			Name:  MetricsEnvVarKubernetesEnvironment,
			Value: state.Metrics.CloudEnvironment,
		})
	}

	return envvars
}

// consoleImageTag returns the image tag for the console container.
func consoleImageTag(state *RenderState) string {
	tag := state.Values.Image.Tag
	if tag == "" {
		tag = AppVersion
	}
	return tag
}
