// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package pipeline

import (
	"context"
	"fmt"

	"github.com/cockroachdb/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	// Default resource requests for Connect pods.
	defaultMemoryRequest = "256Mi"
	defaultCPURequest    = "100m"

	zoneTopologyKey = "topology.kubernetes.io/zone"
)

// render implements [kube.Renderer] for Pipeline resources.
type render struct {
	pipeline          *redpandav1alpha2.Pipeline
	labels            map[string]string
	commonAnnotations map[string]string
	monitoring        MonitoringConfig
	// clusterConn holds resolved Redpanda cluster connection details when
	// the Pipeline references a cluster via clusterRef. Nil when no
	// clusterRef is configured.
	clusterConn *clusterConnection
}

// Types returns the set of Kubernetes resource types managed by the Pipeline
// controller.
func Types() []kube.Object {
	return []kube.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
		&monitoringv1.PodMonitor{},
	}
}

func (r *render) Types() []kube.Object {
	return Types()
}

// Render produces all Kubernetes objects for the given Pipeline.
func (r *render) Render(_ context.Context) ([]kube.Object, error) {
	cm, err := r.configMap()
	if err != nil {
		return nil, err
	}

	dp := r.deployment()

	objs := []kube.Object{cm, dp}

	if pm := r.podMonitor(); pm != nil {
		objs = append(objs, pm)
	}

	return objs, nil
}

// Labels returns the standard set of labels applied to Pipeline-owned
// resources.
func Labels(pipeline *redpandav1alpha2.Pipeline) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "redpanda-connect",
		"app.kubernetes.io/instance":   pipeline.Name,
		"app.kubernetes.io/managed-by": "redpanda-operator",
		"app.kubernetes.io/component":  "connect-pipeline",
	}
}

func (r *render) annotations() map[string]string {
	if len(r.commonAnnotations) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.commonAnnotations))
	for k, v := range r.commonAnnotations {
		out[k] = v
	}
	return out
}

// podAnnotations returns annotations for the pod template, merging
// commonAnnotations with per-pipeline spec.annotations. Per-pipeline
// annotations take precedence.
func (r *render) podAnnotations() map[string]string {
	specAnn := r.pipeline.Spec.Annotations
	if len(r.commonAnnotations) == 0 && len(specAnn) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.commonAnnotations)+len(specAnn))
	for k, v := range r.commonAnnotations {
		out[k] = v
	}
	for k, v := range specAnn {
		out[k] = v
	}
	return out
}

func (r *render) configMap() (*corev1.ConfigMap, error) {
	data := map[string]string{
		"connect.yaml": r.pipeline.Spec.ConfigYAML,
	}
	for filename, content := range r.pipeline.Spec.ConfigFiles {
		if filename == "connect.yaml" {
			return nil, errors.New("configFiles cannot contain a key named \"connect.yaml\"; use configYaml instead")
		}
		data[filename] = content
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Data: data,
	}, nil
}

const (
	// clusterTLSVolumeName is the volume name for the CA certificate from the referenced cluster.
	clusterTLSVolumeName = "cluster-tls-ca"
	// clusterTLSMountPath is the mount path for the cluster CA certificate.
	clusterTLSMountPath = "/etc/tls/certs/ca"
)

func (r *render) deployment() *appsv1.Deployment {
	replicas := r.pipeline.GetReplicas()
	image := r.pipeline.GetImage()

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(defaultMemoryRequest),
			corev1.ResourceCPU:    resource.MustParse(defaultCPURequest),
		},
	}
	if r.pipeline.Spec.Resources != nil {
		resources = *r.pipeline.Spec.Resources
	}

	env := append([]corev1.EnvVar{}, r.pipeline.Spec.Env...)
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: r.pipeline.Name,
					},
				},
			},
		},
	}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/config",
			ReadOnly:  true,
		},
	}

	// Inject cluster connection env vars and TLS volumes when clusterRef is set.
	if cc := r.clusterConn; cc != nil {
		clusterEnv, clusterVolumes, clusterMounts := buildClusterConnectionResources(cc)
		env = append(env, clusterEnv...)
		volumes = append(volumes, clusterVolumes...)
		volumeMounts = append(volumeMounts, clusterMounts...)
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      r.labels,
			Annotations: r.annotations(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: r.labels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      r.labels,
					Annotations: r.podAnnotations(),
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:                     "lint",
							Image:                    image,
							Command:                  []string{"/redpanda-connect", "lint", "/config/connect.yaml"},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							VolumeMounts:             volumeMounts,
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "connect",
							Image:   image,
							Command: []string{"/redpanda-connect", "run", "/config/connect.yaml"},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 4195, Protocol: corev1.ProtocolTCP},
							},
							Env:       env,
							EnvFrom:   buildEnvFrom(r.pipeline),
							Resources: resources,
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ready",
										Port: intstr.FromInt32(4195),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							VolumeMounts: volumeMounts,
						},
					},
					Volumes:                   volumes,
					NodeSelector:              r.pipeline.Spec.NodeSelector,
					Tolerations:               r.pipeline.Spec.Tolerations,
					Affinity:                  buildAffinity(r.pipeline),
					TopologySpreadConstraints: buildTopologySpreadConstraints(r.pipeline, r.labels),
				},
			},
		},
	}
}

// buildClusterConnectionResources returns the env vars, volumes, and volume
// mounts needed to connect to a Redpanda cluster via clusterRef.
func buildClusterConnectionResources(cc *clusterConnection) ([]corev1.EnvVar, []corev1.Volume, []corev1.VolumeMount) {
	var env []corev1.EnvVar
	var volumes []corev1.Volume
	var mounts []corev1.VolumeMount

	env = append(env, corev1.EnvVar{
		Name:  "RPK_BROKERS",
		Value: cc.BrokersString(),
	})

	if cc.TLS != nil {
		caCertPath := fmt.Sprintf("%s/%s", clusterTLSMountPath, cc.TLS.CACertSecretRef.Key)

		env = append(env, corev1.EnvVar{
			Name:  "RPK_TLS_ENABLED",
			Value: "true",
		}, corev1.EnvVar{
			Name:  "RPK_TLS_ROOT_CAS_FILE",
			Value: caCertPath,
		})

		volumes = append(volumes, corev1.Volume{
			Name: clusterTLSVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: cc.TLS.CACertSecretRef.Name,
								},
								Items: []corev1.KeyToPath{
									{
										Key:  cc.TLS.CACertSecretRef.Key,
										Path: cc.TLS.CACertSecretRef.Key,
									},
								},
							},
						},
					},
				},
			},
		})

		mounts = append(mounts, corev1.VolumeMount{
			Name:      clusterTLSVolumeName,
			MountPath: clusterTLSMountPath,
			ReadOnly:  true,
		})
	} else {
		env = append(env, corev1.EnvVar{
			Name:  "RPK_TLS_ENABLED",
			Value: "false",
		})
	}

	if cc.SASL != nil {
		// Use different env var prefixes depending on whether credentials
		// come from spec.credentials (dedicated user) or the cluster's
		// bootstrap admin user.
		prefix := "RPK_SASL"
		if cc.SASL.FromCredentials {
			prefix = "RPK_CREDENTIALS_SASL"
		}

		env = append(env, corev1.EnvVar{
			Name:  prefix + "_MECHANISM",
			Value: cc.SASL.Mechanism,
		}, corev1.EnvVar{
			Name:  prefix + "_USER",
			Value: cc.SASL.Username,
		})

		if cc.SASL.Password.Name != "" {
			env = append(env, cc.SASL.Password)
		}
	}

	return env, volumes, mounts
}

func (r *render) podMonitor() *monitoringv1.PodMonitor {
	if !r.monitoring.Enabled {
		return nil
	}

	labels := make(map[string]string, len(r.labels)+len(r.monitoring.Labels))
	for k, v := range r.labels {
		labels[k] = v
	}
	for k, v := range r.monitoring.Labels {
		labels[k] = v
	}

	endpoint := monitoringv1.PodMetricsEndpoint{
		Path: "/metrics",
		Port: ptr.To("http"),
	}
	if r.monitoring.ScrapeInterval != "" {
		endpoint.Interval = monitoringv1.Duration(r.monitoring.ScrapeInterval)
	}

	return &monitoringv1.PodMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PodMonitor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.pipeline.Name,
			Namespace:   r.pipeline.Namespace,
			Labels:      labels,
			Annotations: r.annotations(),
		},
		Spec: monitoringv1.PodMonitorSpec{
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: r.labels,
			},
		},
	}
}

// buildEnvFrom constructs EnvFromSource entries for each referenced Secret,
// injecting all key-value pairs as environment variables into the container.
func buildEnvFrom(pipeline *redpandav1alpha2.Pipeline) []corev1.EnvFromSource {
	if len(pipeline.Spec.SecretRef) == 0 {
		return nil
	}

	envFrom := make([]corev1.EnvFromSource, 0, len(pipeline.Spec.SecretRef))
	for _, ref := range pipeline.Spec.SecretRef {
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: ref,
			},
		})
	}
	return envFrom
}

// buildAffinity constructs a node affinity that restricts pods to the specified
// zones. Returns nil if no zones are configured.
func buildAffinity(connect *redpandav1alpha2.Pipeline) *corev1.Affinity {
	if len(connect.Spec.Zones) == 0 {
		return nil
	}

	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      zoneTopologyKey,
								Operator: corev1.NodeSelectorOpIn,
								Values:   connect.Spec.Zones,
							},
						},
					},
				},
			},
		},
	}
}

// buildTopologySpreadConstraints returns the topology spread constraints for
// the pipeline. If zones are configured and no explicit constraints are
// provided, a default constraint is generated to spread pods evenly across
// the specified zones.
func buildTopologySpreadConstraints(connect *redpandav1alpha2.Pipeline, selectorLabels map[string]string) []corev1.TopologySpreadConstraint {
	if len(connect.Spec.TopologySpreadConstraints) > 0 {
		return connect.Spec.TopologySpreadConstraints
	}

	if len(connect.Spec.Zones) > 0 {
		return []corev1.TopologySpreadConstraint{
			{
				MaxSkew:           1,
				TopologyKey:       zoneTopologyKey,
				WhenUnsatisfiable: corev1.ScheduleAnyway,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: selectorLabels,
				},
			},
		}
	}

	return nil
}

// FormatConditionMessage returns a formatted message for a condition update
// during resource reconciliation.
func FormatConditionMessage(resource, detail string) string {
	return fmt.Sprintf("%s reconciliation failed: %v", resource, detail)
}
