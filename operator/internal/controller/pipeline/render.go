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
	pipeline *redpandav1alpha2.Pipeline
	labels   map[string]string
}

// Types returns the set of Kubernetes resource types managed by the Pipeline
// controller.
func Types() []kube.Object {
	return []kube.Object{
		&appsv1.Deployment{},
		&corev1.ConfigMap{},
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

	return []kube.Object{cm, dp}, nil
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
	if len(r.pipeline.Spec.CommonAnnotations) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.pipeline.Spec.CommonAnnotations))
	for k, v := range r.pipeline.Spec.CommonAnnotations {
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
					Annotations: r.annotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "connect",
							Image:   image,
							Command: []string{"redpanda-connect", "run", "/config/connect.yaml"},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 4195, Protocol: corev1.ProtocolTCP},
							},
							Env:       r.pipeline.Spec.Env,
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
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/config",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
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
					},
					NodeSelector:              r.pipeline.Spec.NodeSelector,
					Tolerations:               r.pipeline.Spec.Tolerations,
					Affinity:                  buildAffinity(r.pipeline),
					TopologySpreadConstraints: buildTopologySpreadConstraints(r.pipeline, r.labels),
				},
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
