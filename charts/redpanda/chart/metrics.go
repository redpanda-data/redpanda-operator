// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
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

// MetricsEnvironmentVariables returns the redpanda container's telemetry env
// vars: the REDPANDA_METRICS_K8S_* values, or, for Redpanda versions that
// read them from MetricsConfigMap, only the directory to read them from.
func MetricsEnvironmentVariables(state *RenderState, pool Pool) []corev1.EnvVar {
	if metricsFromDir(state) {
		return redpanda.MetricsDirEnv()
	}
	return metricsValues(state, pool)
}

// MetricsConfigMap returns the pool's ConfigMap of REDPANDA_METRICS_K8S_*
// values, or nil when they're passed as env vars.
func MetricsConfigMap(state *RenderState, pool Pool) *corev1.ConfigMap {
	if !metricsFromDir(state) {
		return nil
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        redpanda.MetricsConfigMapName(fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Data: redpanda.MetricsConfigMapData(metricsValues(state, pool)),
	}
}

func metricsFromDir(state *RenderState) bool {
	return metricsEnabled(state) && redpanda.MetricsFromDir(Tag(state))
}

func metricsEnabled(state *RenderState) bool {
	// enable_metrics_reporter defaults to true, so unless someone has explicitly opted out
	// let's attempt to inject additional context into the metrics
	coreMetricsEnabled := helmette.Dig(state.Values.Config.Cluster, true, "enable_metrics_reporter").(bool)

	// secondarily let's attempt to check the usage stats flag that appears
	// to be used nowhere, this is mainly as a fail-safe so that
	// if various dynamic lookup components can't succeed (i.e. the UID)
	// determination below, we can still succeed in deploying the cluster
	// by disabling this flag on our values.
	return coreMetricsEnabled && state.Values.Logging.UsageStats.Enabled
}

func metricsValues(state *RenderState, pool Pool) []corev1.EnvVar {
	if !metricsEnabled(state) {
		return nil
	}

	deploymentType := deploymentTypeHelm
	if state.ViaOperator {
		deploymentType = deploymentTypeOperator
	}
	chartVersion := state.Dot.Chart.Version
	if state.OperatorVersion != "" {
		chartVersion = state.OperatorVersion
	}

	envvars := []corev1.EnvVar{{
		Name:  MetricsEnvVarKubernetesVersion,
		Value: state.Dot.Capabilities.KubeVersion.Version,
	}, {
		Name:  MetricsEnvVarDeploymentType,
		Value: deploymentType,
	}, {
		Name:  MetricsEnvVarChartVersion,
		Value: chartVersion,
	}, {
		Name:  MetricsEnvVarOperatorVersion,
		Value: fmt.Sprintf(`%s:%s`, pool.Statefulset.SideCars.Image.Repository, pool.Statefulset.SideCars.Image.Tag),
	}}

	// UID of the kube-system namespace to fingerprint the cluster: https://opentelemetry.io/docs/specs/semconv/resource/k8s/#cluster
	//nolint:laconiccomments
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
