// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

func checkServiceWithPort(ctx context.Context, t framework.TestingT, serviceName, portName string, port int32) {
	var service corev1.Service

	key := t.ResourceKey(serviceName)

	t.Logf("Checking service %q has port %q with value %d", serviceName, portName, port)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &service))

		for _, servicePort := range service.Spec.Ports {
			if servicePort.Name == portName {
				hasMatchingPort := servicePort.Port == port
				t.Logf(`Checking port %q has value %d? %v (%d)`, portName, port, hasMatchingPort, servicePort.Port)
				return hasMatchingPort
			}
		}

		t.Logf(`Did not find port named %q`, portName)
		return false
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf(`Service %q never contained port named %q with value %d`, key.String(), portName, port)
	}))
	t.Logf("Found port named %q on service %q with value %d!", portName, serviceName, port)
}

// serviceShouldHaveNoSelector asserts a Service the operator steers has shed
// its selector, which is what keeps the native EndpointSlice controller from
// publishing every broker on every port alongside the operator.
func serviceShouldHaveNoSelector(ctx context.Context, t framework.TestingT, serviceName string) {
	var service corev1.Service
	key := t.ResourceKey(serviceName)

	t.Logf("Checking service %q has no selector", serviceName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, key, &service))
		return len(service.Spec.Selector) == 0
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf("Service %q kept its selector %v", key.String(), service.Spec.Selector)
	}))
}

// serviceShouldHaveSelector asserts a Service is still the native
// EndpointSlice controller's to publish.
func serviceShouldHaveSelector(ctx context.Context, t framework.TestingT, serviceName string) {
	var service corev1.Service
	require.NoError(t, t.Get(ctx, t.ResourceKey(serviceName), &service))
	require.NotEmpty(t, service.Spec.Selector, "service %q lost its selector", serviceName)
}

// servicePortShouldPublishPods asserts that the EndpointSlices the operator
// publishes for a Service eventually list exactly the given pods (comma
// separated) for the named port -- and no others, which is the whole point
// of steering a port.
func servicePortShouldPublishPods(ctx context.Context, t framework.TestingT, portName, serviceName, podList string) {
	want := sets.List(sets.New(strings.Fields(strings.ReplaceAll(podList, ",", " "))...))
	var got []string

	t.Logf("Checking port %q of service %q publishes pods %v", portName, serviceName, want)
	published := assert.Eventually(t, func() bool {
		endpointSlices, err := operatorManagedSlices(ctx, t, serviceName)
		if err != nil {
			t.Logf("Failed to list EndpointSlices of service %q: %v", serviceName, err)
			return false
		}
		got = publishedPods(endpointSlices, portName)
		return slices.Equal(got, want)
	}, 5*time.Minute, 5*time.Second)
	if !published {
		// Which pods a port ends up with is a conclusion the operator
		// reached from the Service, the pods, and a probe of each one, so
		// report all three rather than only the conclusion. The shared
		// operator's own log is dumped for every feature, but a V1 cluster's
		// reconciler alone outruns the tail, so the lines about this Service
		// have to be pulled out by name.
		dumpSteeringDiagnostics(ctx, t, serviceName)
		require.Failf(t, "endpoints never matched",
			"port %q of service %q published %v, wanted %v", portName, serviceName, got, want)
	}
	t.Logf("Port %q of service %q publishes pods %v!", portName, serviceName, want)
}

// serviceOfClusterShouldHaveNoSelector asserts the cluster's own steered
// Service has shed its selector.
func serviceOfClusterShouldHaveNoSelector(ctx context.Context, t framework.TestingT, clusterName string) {
	service, _ := steeredServiceOf(t, clusterName)
	serviceShouldHaveNoSelector(ctx, t, service)
}

// unsteeredServicesOfClusterShouldHaveSelectors asserts the cluster's other
// Services stay with the native EndpointSlice controller: on V1 the headless
// Service, which carries broker discovery alone. A V2 cluster serves every
// listener on the one steered Service, so it has none.
func unsteeredServicesOfClusterShouldHaveSelectors(ctx context.Context, t framework.TestingT, clusterName string) {
	if getVersion(t, "") != "vectorized" {
		t.Logf("Cluster %q serves every listener on its steered Service; no other service to check", clusterName)
		return
	}
	serviceShouldHaveSelector(ctx, t, clusterName)
}

// clusterListenerShouldPublishPods is servicePortShouldPublishPods against
// the cluster's own steered Service, naming a listener rather than a port so
// that one scenario reads the same for both cluster APIs.
func clusterListenerShouldPublishPods(ctx context.Context, t framework.TestingT, listener, clusterName, podList string) {
	service, ports := steeredServiceOf(t, clusterName)
	port, ok := ports[listener]
	require.Truef(t, ok, "no port known for the %q listener", listener)
	servicePortShouldPublishPods(ctx, t, port, service, podList)
}

// steeredServiceOf is the Service carrying a cluster's Schema Registry
// listener -- the one the operator is asked to steer -- and the names its
// ports go by, which the two cluster APIs spell differently.
func steeredServiceOf(t framework.TestingT, clusterName string) (string, map[string]string) {
	if getVersion(t, "") == "vectorized" {
		// A V1 cluster serves Schema Registry on its ClusterIP Service; the
		// headless one carries broker discovery alone and is left to the
		// native EndpointSlice controller.
		return clusterName + "-cluster", map[string]string{
			"kafka":           vectorizedv1alpha1.InternalListenerName,
			"schema registry": resources.SchemaRegistryPortName,
		}
	}
	return clusterName, map[string]string{
		"kafka":           redpandachart.InternalKafkaPortName,
		"schema registry": redpandachart.InternalSchemaRegistryPortName,
	}
}

// dumpSteeringDiagnostics reports everything that decides a steered
// Service's endpoints: the Service itself, every slice published for it
// whoever owns it, any NetworkPolicy that could be shaping the probes, and
// the operator's own account of the decision.
func dumpSteeringDiagnostics(ctx context.Context, t framework.TestingT, serviceName string) {
	var service corev1.Service
	if err := t.Get(ctx, t.ResourceKey(serviceName), &service); err != nil {
		t.Logf("[steering] service %q: %v", serviceName, err)
	} else {
		t.Logf("[steering] service %q: selector=%v publishNotReadyAddresses=%v annotations=%v ports=%s",
			serviceName, service.Spec.Selector, service.Spec.PublishNotReadyAddresses, service.Annotations, formatServicePorts(service.Spec.Ports))
	}

	var endpointSlices discoveryv1.EndpointSliceList
	if err := t.List(ctx, &endpointSlices, runtimeclient.InNamespace(t.Namespace()), runtimeclient.MatchingLabels{
		discoveryv1.LabelServiceName: serviceName,
	}); err != nil {
		t.Logf("[steering] listing EndpointSlices of service %q: %v", serviceName, err)
	}
	for i := range endpointSlices.Items {
		slice := &endpointSlices.Items[i]
		t.Logf("[steering] endpointslice %q: managed-by=%q ports=%s endpoints=%s",
			slice.Name, slice.Labels[discoveryv1.LabelManagedBy], formatSlicePorts(slice.Ports), formatSliceEndpoints(slice.Endpoints))
	}

	var pods corev1.PodList
	if err := t.List(ctx, &pods, runtimeclient.InNamespace(t.Namespace()), runtimeclient.MatchingLabels(service.Spec.Selector)); err != nil {
		t.Logf("[steering] listing pods: %v", err)
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		t.Logf("[steering] pod %q: phase=%s address=%s subdomain=%q node=%q labels=%v",
			pod.Name, pod.Status.Phase, pod.Status.PodIP, pod.Spec.Subdomain, pod.Spec.NodeName, pod.Labels)
	}

	dumpOperatorLogsMatching(ctx, t, serviceName)
}

func formatServicePorts(ports []corev1.ServicePort) string {
	descriptions := make([]string, len(ports))
	for i, port := range ports {
		descriptions[i] = fmt.Sprintf("%s/%s:%d->%s", port.Name, port.Protocol, port.Port, port.TargetPort.String())
	}
	return "[" + strings.Join(descriptions, " ") + "]"
}

func formatSlicePorts(ports []discoveryv1.EndpointPort) string {
	descriptions := make([]string, len(ports))
	for i, port := range ports {
		descriptions[i] = fmt.Sprintf("%s:%d", ptr.Deref(port.Name, ""), ptr.Deref(port.Port, 0))
	}
	return "[" + strings.Join(descriptions, " ") + "]"
}

func formatSliceEndpoints(endpoints []discoveryv1.Endpoint) string {
	descriptions := make([]string, len(endpoints))
	for i, endpoint := range endpoints {
		var name string
		if endpoint.TargetRef != nil {
			name = endpoint.TargetRef.Name
		}
		descriptions[i] = fmt.Sprintf("%s(%v ready=%v serving=%v terminating=%v)", name, endpoint.Addresses,
			ptr.Deref(endpoint.Conditions.Ready, false), ptr.Deref(endpoint.Conditions.Serving, false), ptr.Deref(endpoint.Conditions.Terminating, false))
	}
	return "[" + strings.Join(descriptions, " ") + "]"
}

// operatorManagedSlices lists the EndpointSlices the operator's endpoint
// steering controller publishes for a Service.
func operatorManagedSlices(ctx context.Context, t framework.TestingT, serviceName string) ([]discoveryv1.EndpointSlice, error) {
	var list discoveryv1.EndpointSliceList
	err := t.List(ctx, &list, runtimeclient.InNamespace(t.Namespace()), runtimeclient.MatchingLabels{
		discoveryv1.LabelServiceName: serviceName,
		// endpointsteering.ManagedBy, spelled out: this module cannot import
		// operator/internal.
		discoveryv1.LabelManagedBy: "redpanda-operator",
	})
	return list.Items, err
}

// publishedPods returns, sorted, the pods endpointSlices publish for the
// named Service port.
func publishedPods(endpointSlices []discoveryv1.EndpointSlice, portName string) []string {
	pods := sets.New[string]()
	for _, slice := range endpointSlices {
		if !slices.ContainsFunc(slice.Ports, func(port discoveryv1.EndpointPort) bool {
			return ptr.Deref(port.Name, "") == portName
		}) {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef != nil {
				pods.Insert(endpoint.TargetRef.Name)
			}
		}
	}
	return sets.List(pods)
}
