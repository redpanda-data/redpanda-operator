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
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/kube/servicetemplate"
)

// perPodServices returns per-pod ClusterIP Services for stable DNS resolution.
// Each pod in each pool gets its own service, named "{pool-name}-{ordinal}".
func perPodServices(state *RenderState) ([]*corev1.Service, error) {
	var services []*corev1.Service
	for _, pool := range state.pools {
		isLocal := state.isLocalPool(pool)
		override := perPodServiceOverride(pool, isLocal)
		if !override.IsEnabled() {
			continue
		}
		for i := int32(0); i < pool.GetReplicas(); i++ {
			svc, err := perPodService(state, pool, i, override)
			if err != nil {
				return nil, err
			}
			services = append(services, svc)
		}
	}
	return services, nil
}

func perPodService(state *RenderState, pool *redpandav1alpha2.NodePool, ordinal int32, override *redpandav1alpha2.PerPodServiceOverride) (*corev1.Service, error) {
	spec := state.Spec()

	labels := state.commonLabels()
	labels[labelMonitorKey] = fmt.Sprintf("%t", spec.Monitoring.IsEnabled())

	ports := perPodServicePorts(spec)

	name := PerPodServiceName(pool, ordinal)
	annotations := make(map[string]string)
	if spec.Service != nil && spec.Service.Internal != nil {
		// TODO: consider a special field for per pod service annotation, either in nodepool or stretchcluster spec.
		annotations = spec.Service.Internal.Annotations
	}
	// make sure this service only selects one pod
	selector := perPodServiceSelector(state, pool, ordinal)

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   state.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			PublishNotReadyAddresses: true,
			Selector:                 selector,
			Ports:                    ports,
			IPFamilyPolicy:           ptr.To(corev1.IPFamilyPolicySingleStack),
		},
	}

	// Apply per-pod service overrides from the pool spec.
	if override != nil {
		merged, err := servicetemplate.StrategicMergePatch(servicetemplate.Overrides{
			Labels:      override.Labels,
			Annotations: override.Annotations,
			Spec:        override.Spec,
		}, *svc)
		if err != nil {
			return nil, fmt.Errorf("applying per-pod service overrides for %s: %w", name, err)
		}
		svc = &merged
	}

	return svc, nil
}

// perPodServiceOverride returns the applicable override for a per-pod Service,
// based on whether the pool is local or remote.
func perPodServiceOverride(pool *redpandav1alpha2.NodePool, isLocal bool) *redpandav1alpha2.PerPodServiceOverride {
	if pool.Spec.Services == nil || pool.Spec.Services.PerPod == nil {
		return nil
	}
	if isLocal {
		return pool.Spec.Services.PerPod.Local
	}
	return pool.Spec.Services.PerPod.Remote
}

func PerPodServiceName(pool *redpandav1alpha2.NodePool, ordinal int32) string {
	// Service name is the pool name + ordinal (no cluster prefix).
	return fmt.Sprintf("%s-%d", pool.Suffix()[1:], ordinal) // pool.Suffix() returns "-poolname", strip leading dash
}

func perPodServicePorts(spec *redpandav1alpha2.StretchClusterSpec) []corev1.ServicePort {
	var ports []corev1.ServicePort

	adminPort := spec.AdminPort()
	ports = append(ports, corev1.ServicePort{
		Name:       internalAdminAPIPortName,
		Protocol:   corev1.ProtocolTCP,
		Port:       adminPort,
		TargetPort: intstr.FromInt32(adminPort),
	})

	if l := spec.Listeners; l != nil && l.HTTP != nil && l.HTTP.IsEnabled() {
		httpPort := spec.HTTPPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalPandaProxyPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       httpPort,
			TargetPort: intstr.FromInt32(httpPort),
		})
	}

	kafkaPort := spec.KafkaPort()
	ports = append(ports, corev1.ServicePort{
		Name:       internalKafkaPortName,
		Protocol:   corev1.ProtocolTCP,
		Port:       kafkaPort,
		TargetPort: intstr.FromInt32(kafkaPort),
	})

	rpcPort := spec.RPCPort()
	ports = append(ports, corev1.ServicePort{
		Name:       internalRPCPortName,
		Protocol:   corev1.ProtocolTCP,
		Port:       rpcPort,
		TargetPort: intstr.FromInt32(rpcPort),
	})

	if l := spec.Listeners; l != nil && l.SchemaRegistry != nil && l.SchemaRegistry.IsEnabled() {
		srPort := spec.SchemaRegistryPort()
		ports = append(ports, corev1.ServicePort{
			Name:       internalSchemaRegistryPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       srPort,
			TargetPort: intstr.FromInt32(srPort),
		})
	}

	return ports
}

func perPodServiceSelector(state *RenderState, pool *redpandav1alpha2.NodePool, ordinal int32) map[string]string {
	selector := statefulSetPodLabelsSelector(state, pool)
	// make sure this service only selects one pod
	selector["apps.kubernetes.io/pod-index"] = strconv.Itoa(int(ordinal))
	return selector
}
