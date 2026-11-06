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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const (
	// MetricsDirEnvVar points Redpanda at MetricsMountPath, where it reads
	// each REDPANDA_METRICS_K8S_* value from a file of that name on every
	// telemetry report.
	MetricsDirEnvVar = "REDPANDA_METRICS_K8S_DIR"
	// MetricsMountPath is where a pool's metrics ConfigMap is mounted.
	MetricsMountPath = "/etc/redpanda-k8s-metrics"

	metricsVolumeName = "k8s-metrics"
	// metricsDirMinVersion is the first Redpanda release that reads
	// MetricsDirEnvVar (CORE-18071).
	metricsDirMinVersion = ">=26.3.0-0"
)

// MetricsFromDir reports whether a broker on Redpanda version tag reads its
// telemetry values from MetricsMountPath instead of env vars, so they can
// change (operator and Kubernetes upgrades) without changing, and rolling,
// the pod template. Tags that aren't semver keep the env vars.
func MetricsFromDir(tag string) bool {
	ok, err := helmette.SemverCompare(metricsDirMinVersion, tag)
	return err == nil && ok
}

// MetricsConfigMapName returns the name of the metrics ConfigMap of the pool
// whose resources are named poolFullname.
func MetricsConfigMapName(poolFullname string) string {
	return fmt.Sprintf("%.51s-k8s-metrics", poolFullname)
}

// MetricsConfigMapData returns the metrics ConfigMap's data: one key per
// REDPANDA_METRICS_K8S_* value. The ConfigMap stays out of every checksum
// annotation, as updating it must not roll brokers.
func MetricsConfigMapData(values []corev1.EnvVar) map[string]string {
	data := map[string]string{}
	for _, value := range values {
		data[value.Name] = value.Value
	}
	return data
}

// MetricsVolume mounts the metrics ConfigMap as a whole volume (no subPath),
// so kubelet keeps the files in sync. It's optional: telemetry must never
// keep a broker from starting.
func MetricsVolume(poolFullname string) corev1.Volume {
	return corev1.Volume{
		Name: metricsVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: MetricsConfigMapName(poolFullname)},
				Optional:             ptr.To(true),
			},
		},
	}
}

// MetricsVolumeMount mounts MetricsVolume into the redpanda container.
func MetricsVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{Name: metricsVolumeName, MountPath: MetricsMountPath, ReadOnly: true}
}

// MetricsDirEnv is the redpanda container's only telemetry env var when
// MetricsFromDir holds.
func MetricsDirEnv() []corev1.EnvVar {
	return []corev1.EnvVar{{Name: MetricsDirEnvVar, Value: MetricsMountPath}}
}
