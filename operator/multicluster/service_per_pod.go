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
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/kube/servicetemplate"
)

// perPodServices returns per-pod ClusterIP Services for stable DNS resolution
// across every pool in every region.
// Each pod in each pool gets its own service, named "{pool-name}-{ordinal}".
func perPodServices(state *RenderState) ([]*corev1.Service, error) {
	var services []*corev1.Service
	for _, pool := range state.pools {
		svcs, err := perPodServicesForPool(state, pool)
		if err != nil {
			return nil, err
		}
		services = append(services, svcs...)
	}
	return services, nil
}

// perPodServicesForPool returns the per-pod ClusterIP Services for a single
// BrokerPool (local or remote). Caller iterates state.Pools().
func perPodServicesForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) ([]*corev1.Service, error) {
	override := perPodServiceOverride(pool, state.isLocalPool(pool))
	if !override.IsEnabled() {
		return nil, nil
	}

	var brokers []redpanda.BrokerService
	for i := int32(0); i < pool.GetReplicas(); i++ {
		// In flat network mode, all per-pod Services are headless and have no
		// selectors. The controller manages EndpointSlices that contain the pod
		// IPs of the local and the remote pods.
		var selector map[string]string
		if !state.Spec().Networking.IsFlatNetwork() {
			selector = perPodServiceSelector(state, pool, i)
		}

		brokers = append(brokers, redpanda.BrokerService{
			Name:     PerPodServiceName(state.poolFullname(pool), i),
			Selector: selector,
		})
	}

	config := perPodServiceConfig(state, pool)
	config.Brokers = brokers
	services := config.Render()

	if override == nil {
		return services, nil
	}

	for i, svc := range services {
		merged, err := servicetemplate.StrategicMergePatch(servicetemplate.Overrides{
			Labels:      override.Labels,
			Annotations: override.Annotations,
			Spec:        override.Spec,
		}, *svc)
		if err != nil {
			return nil, fmt.Errorf("applying per-pod service overrides for %s: %w", svc.Name, err)
		}
		services[i] = &merged
	}

	return services, nil
}

// perPodServicePorts returns the ports that each per-pod Service of pool
// publishes.
func perPodServicePorts(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) []corev1.ServicePort {
	// NB: The result is never nil, because the admin port is always published.
	config := perPodServiceConfig(state, pool)
	return config.Ports()
}

func perPodServiceConfig(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) redpanda.ServiceConfig {
	spec := state.Spec()

	labels := state.commonLabels()
	labels[labelMonitorKey] = fmt.Sprintf("%t", pool.Spec.Monitoring.IsEnabled())

	var clusterIP string
	if spec.Networking.IsFlatNetwork() {
		clusterIP = corev1.ClusterIPNone
	}

	listeners := poolListeners(state, pool)
	return redpanda.ServiceConfig{
		Kind:      redpanda.ServiceKindBroker,
		Listeners: withoutDisabled(listeners.InCluster(), pool, []redpanda.APIKind{redpanda.HTTPAPI, redpanda.SchemaRegistryAPI}),
		Template: corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: state.namespace,
				Labels:    labels,
				// The internal Service annotations apply to the full cluster, because
				// the headless Service includes all pools.
				Annotations: spec.InternalServiceAnnotations,
			},
			Spec: corev1.ServiceSpec{
				Type:                     corev1.ServiceTypeClusterIP,
				ClusterIP:                clusterIP,
				PublishNotReadyAddresses: true,
				IPFamilyPolicy:           ptr.To(corev1.IPFamilyPolicySingleStack),
			},
		},
	}
}

// perPodServiceOverride returns the applicable override for a per-pod Service,
// based on whether the pool is local or remote.
func perPodServiceOverride(pool *redpandav1alpha2.RedpandaBrokerPool, isLocal bool) *redpandav1alpha2.PerPodServiceOverride {
	if pool.Spec.Services == nil || pool.Spec.Services.PerPod == nil {
		return nil
	}
	if isLocal {
		return pool.Spec.Services.PerPod.Local
	}
	return pool.Spec.Services.PerPod.Remote
}

func PerPodServiceName(poolFullname string, ordinal int32) string {
	return fmt.Sprintf("%s-%d", poolFullname, ordinal)
}

// BrokerPodSelector returns the labels identifying the broker Pods of the
// cluster deployed under releaseName -- sc.Name for a StretchCluster.
//
// These are StatefulSet selector labels: immutable, so present on every broker
// Pod whatever operator version rendered it. Stricter markers such as
// cluster.redpanda.com/broker live only on the Pod template, and filtering on
// one would drop a live broker rendered before the label existed.
func BrokerPodSelector(releaseName string) map[string]string {
	return map[string]string{
		labelNameKey:     labelNameValue,
		labelInstanceKey: releaseName,
	}
}

func perPodServiceSelector(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool, ordinal int32) map[string]string {
	selector := statefulSetPodLabelsSelector(state, pool)
	// make sure this service only selects one pod
	selector["apps.kubernetes.io/pod-index"] = strconv.Itoa(int(ordinal))
	return selector
}
