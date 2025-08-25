// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
)

func LoadBalancerServices(dot *helmette.Dot, pools []*redpandav1alpha3.NodePool) []*corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	// This is technically a divergence from previous behavior but this matches
	// the NodePort's check and is more reasonable.
	if !values.External.Enabled || !values.External.Service.Enabled {
		return nil
	}

	if values.External.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}

	externalDNS := ptr.Deref(values.External.ExternalDNS, Enableable{})

	labels := FullLabels(dot)

	// This typo is intentionally being preserved for backwards compat
	// https://github.com/redpanda-data/helm-charts/blob/2baa77b99a71a993e639a7138deaf4543727c8a1/charts/redpanda/templates/service.loadbalancer.yaml#L33
	labels["repdanda.com/type"] = "loadbalancer"

	selector := ClusterPodLabelsSelector(dot)

	var services []*corev1.Service

	renderService := func(pool *redpandav1alpha3.NodePool, i int32) {
		podname := fmt.Sprintf("%s-%d", Fullname(dot), i)
		if pool != nil {
			podname = fmt.Sprintf("%s-%s-%d", Fullname(dot), pool.Name, i)
		}

		// NB: A range loop is used here as its the most terse way to handle
		// nil maps in gotohelm.
		annotations := map[string]string{}
		for k, v := range helmette.SortedMap(values.External.Annotations) {
			annotations[k] = v
		}

		if externalDNS.Enabled {
			prefix := podname
			if len(values.External.Addresses) > 0 {
				if len(values.External.Addresses) == 1 {
					prefix = values.External.Addresses[0]
				} else {
					prefix = values.External.Addresses[i]
				}
			}

			address := fmt.Sprintf("%s.%s", prefix, helmette.Tpl(dot, *values.External.Domain, dot))

			annotations["external-dns.alpha.kubernetes.io/hostname"] = address
		}

		// NB: A range loop is used here as its the most terse way to handle
		// nil maps in gotohelm.
		podSelector := map[string]string{}
		for k, v := range selector {
			podSelector[k] = v
		}

		podSelector["statefulset.kubernetes.io/pod-name"] = podname

		// Divergences pop up here due to iterating over a map. This isn't okay
		// in helm. TODO setup a linter that barks about this? Also a helper
		// for getting the sorted keys of a map?
		var ports []corev1.ServicePort
		ports = append(ports, values.Listeners.Admin.ServicePorts("admin", &values.External)...)
		ports = append(ports, values.Listeners.Kafka.ServicePorts("kafka", &values.External)...)
		ports = append(ports, values.Listeners.HTTP.ServicePorts("http", &values.External)...)
		ports = append(ports, values.Listeners.SchemaRegistry.ServicePorts("schema", &values.External)...)

		svc := &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("lb-%s", podname),
				Namespace:   dot.Release.Namespace,
				Labels:      labels,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				ExternalTrafficPolicy:    corev1.ServiceExternalTrafficPolicyLocal,
				LoadBalancerSourceRanges: values.External.SourceRanges,
				Ports:                    ports,
				PublishNotReadyAddresses: true,
				Selector:                 podSelector,
				SessionAffinity:          corev1.ServiceAffinityNone,
				Type:                     corev1.ServiceTypeLoadBalancer,
			},
		}

		services = append(services, svc)
	}
	for i := int32(0); i < values.Statefulset.Replicas; i++ {
		renderService(nil, i)
	}
	for _, pool := range pools {
		for i := int32(0); i < ptr.Deref(pool.Spec.Replicas, 0); i++ {
			renderService(pool, i)
		}
	}

	return services
}
