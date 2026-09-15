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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	framework "github.com/redpanda-data/redpanda-operator/harpoon"
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
	require.Eventually(t, func() bool {
		endpointSlices, err := operatorManagedSlices(ctx, t, serviceName)
		if err != nil {
			t.Logf("Failed to list EndpointSlices of service %q: %v", serviceName, err)
			return false
		}
		got = publishedPods(endpointSlices, portName)
		return slices.Equal(got, want)
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf("port %q of service %q published %v, wanted %v", portName, serviceName, got, want)
	}))
	t.Logf("Port %q of service %q publishes pods %v!", portName, serviceName, want)
}

// operatorManagedSlices lists the EndpointSlices the operator's endpoint
// steering controller publishes for a Service.
func operatorManagedSlices(ctx context.Context, t framework.TestingT, serviceName string) ([]discoveryv1.EndpointSlice, error) {
	var list discoveryv1.EndpointSliceList
	err := t.List(ctx, &list, runtimeclient.InNamespace(t.Namespace()), runtimeclient.MatchingLabels{
		discoveryv1.LabelServiceName: serviceName,
		discoveryv1.LabelManagedBy:   redpandachart.EndpointSteeringManagedBy,
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
