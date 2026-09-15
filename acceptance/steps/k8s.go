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
	"bytes"
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/jsonpath"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// this is a nasty hack due to the fact that we can't disable the linter for typecheck
// that reports sigs.k8s.io/controller-runtime/pkg/client as unused when it's solely used
// for type assertions
var _ client.Object = (client.Object)(nil)

func podWillEventuallyBeInPhase(ctx context.Context, t framework.TestingT, podName string, phase string) {
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var pod corev1.Pod
		require.NoError(c, t.Get(ctx, t.ResourceKey(podName), &pod))

		require.Equal(c, corev1.PodPhase(phase), pod.Status.Phase)
	}, 5*time.Minute, 5*time.Second)
}

func kubernetesObjectHasClusterOwner(ctx context.Context, t framework.TestingT, groupVersionKind, resourceName, clusterName string) {
	var cluster redpandav1alpha2.Redpanda

	gvk, _ := schema.ParseKindArg(groupVersionKind)
	obj, err := t.Scheme().New(*gvk)
	require.NoError(t, err)

	o := obj.(client.Object)

	require.NoError(t, t.Get(ctx, t.ResourceKey(resourceName), o))

	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, t.ResourceKey(clusterName), &cluster))
		require.NoError(t, t.Get(ctx, t.ResourceKey(resourceName), o))
		cluster.SetGroupVersionKind(redpandav1alpha2.SchemeGroupVersion.WithKind("Redpanda"))

		references := o.GetOwnerReferences()
		if len(references) != 1 {
			t.Logf("object has %d owner references", len(references))
			return false
		}

		actual := references[0]
		expected := cluster.OwnerShipRefObj()

		matchesAPIVersion := actual.APIVersion == expected.APIVersion
		matchesKind := actual.Kind == expected.Kind
		matchesName := actual.Name == expected.Name

		matches := matchesAPIVersion && matchesKind && matchesName

		t.Logf(`Checking object contains cluster owner reference? (actual: %s/%s -> %s) (expected: %s/%s -> %s)`, actual.Kind, actual.APIVersion, actual.Name, expected.Kind, expected.APIVersion, expected.Name)
		return matches
	}, 5*time.Minute, 5*time.Second, "", delayLog(func() string {
		return fmt.Sprintf(`Object %q never contained owner reference for cluster %q, final OwnerReference: %+v`, resourceName, clusterName, o.GetOwnerReferences())
	}))

	t.Logf("Object has cluster owner reference for %q", clusterName)
}

type recordedVariable string

func recordVariable(ctx context.Context, t framework.TestingT, jsonPath, groupVersionKind, resourceName, variableName string) context.Context {
	return context.WithValue(ctx, recordedVariable(variableName), execJSONPath(ctx, t, jsonPath, groupVersionKind, resourceName))
}

func assertVariableValue(ctx context.Context, t framework.TestingT, variableName, jsonPath, groupVersionKind, resourceName string) {
	currentValue := execJSONPath(ctx, t, jsonPath, groupVersionKind, resourceName)
	previousValue := ctx.Value(recordedVariable(variableName))
	require.Equal(t, previousValue, currentValue)
}

func assertVariableValueIncremented(ctx context.Context, t framework.TestingT, variableName, jsonPath, groupVersionKind, resourceName string) {
	currentValue := execJSONPath(ctx, t, jsonPath, groupVersionKind, resourceName)
	previousValue := ctx.Value(recordedVariable(variableName))

	if reflect.TypeOf(previousValue) != reflect.TypeOf(currentValue) {
		t.Fatalf("unmatched types: %T, %T", previousValue, currentValue)
		return
	}

	// check if we're dealing with integer types
	// NOTE: this verbose switch statement is painful
	// but there's not a great way of incrementing
	// a number otherwise via reflection
	switch value := previousValue.(type) {
	case int:
		require.Equal(t, value+1, currentValue)
	case int8:
		require.Equal(t, value+1, currentValue)
	case int16:
		require.Equal(t, value+1, currentValue)
	case int32:
		require.Equal(t, value+1, currentValue)
	case int64:
		require.Equal(t, value+1, currentValue)
	case uint:
		require.Equal(t, value+1, currentValue)
	case uint8:
		require.Equal(t, value+1, currentValue)
	case uint16:
		require.Equal(t, value+1, currentValue)
	case uint32:
		require.Equal(t, value+1, currentValue)
	case uint64:
		require.Equal(t, value+1, currentValue)
	default:
		t.Fatalf("unsupported type: %T", previousValue)
	}
}

func execJSONPath(ctx context.Context, t framework.TestingT, jsonPath, groupVersionKind, resourceName string) any {
	gvk, _ := schema.ParseKindArg(groupVersionKind)

	obj, err := t.Scheme().New(*gvk)
	require.NoError(t, err)

	require.NoError(t, t.Get(ctx, t.ResourceKey(resourceName), obj.(client.Object)))

	// See https://kubernetes.io/docs/reference/kubectl/jsonpath/
	path := jsonpath.New("").AllowMissingKeys(true)
	require.NoError(t, path.Parse(jsonPath))

	results, err := path.FindResults(obj)
	require.NoError(t, err)

	// If jsonPath contains a range loop or {range}{end} block, the result
	// will be an array of values. If not, the results are still an array
	// but test writers would expect it to be a single value.
	resultIsArray := strings.Contains(jsonPath, "*") || strings.Contains(jsonPath, "{range")

	for _, result := range results {
		var unwrapped []any
		for _, x := range result {
			unwrapped = append(unwrapped, x.Interface())
		}

		if resultIsArray {
			return unwrapped
		} else {
			require.Len(t, unwrapped, 1, "non iterating JSON path found multiple results: %s", jsonPath)
			return unwrapped[0]
		}
	}
	return nil
}

func execInPodEventuallyMatches(
	ctx context.Context,
	t framework.TestingT,
	podName string,
	cmd string,
	expected *godog.DocString,
) {
	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	pod, err := kube.Get[corev1.Pod](ctx, ctl, kube.ObjectKey{Namespace: t.Namespace(), Name: podName})
	require.NoErrorf(t, err, "Pod with name %q not found", podName)

	execInPod(t, ctx, ctl, pod, cmd, expected)
}

func execInPodMatchingEventuallyMatches(
	ctx context.Context,
	t framework.TestingT,
	cmd,
	selectorStr string,
	expected *godog.DocString,
) {
	selector, err := labels.Parse(selectorStr)
	require.NoError(t, err)

	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	pods, err := kube.List[corev1.PodList](ctx, ctl, t.Namespace(), client.MatchingLabelsSelector{Selector: selector})
	require.NoError(t, err)

	require.True(t, len(pods.Items) > 0, "selector %q found no Pods", selector.String())

	execInPod(t, ctx, ctl, &pods.Items[0], cmd, expected)
}

func execInPod(
	t framework.TestingT,
	ctx context.Context,
	ctl *kube.Ctl,
	pod *corev1.Pod,
	cmd string,
	expected *godog.DocString,
) {
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var stdout bytes.Buffer
		require.NoError(collect, ctl.Exec(ctx, pod, kube.ExecOptions{
			Command: []string{"sh", "-c", cmd},
			Stdout:  &stdout,
		}))

		assert.Equal(collect, strings.TrimSpace(expected.Content), strings.TrimSpace(stdout.String()))
	}, 5*time.Minute, 5*time.Second)
}

// blockedPodLabel marks the NetworkPolicies iBlockIngressToPortOfPod creates
// with the pod they target, so iUnblockIngressToPod can find them.
const blockedPodLabel = "acceptance.redpanda.com/blocked-pod"

// iBlockIngressToPortOfPod denies traffic to one port of a pod and nothing
// else, through a NetworkPolicy that allows every other TCP port: a Schema
// Registry nobody can reach on a broker that is otherwise healthy and Ready.
func iBlockIngressToPortOfPod(ctx context.Context, t framework.TestingT, port int, podName string) {
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("block-%s-%d", podName, port),
			Namespace: t.Namespace(),
			Labels:    map[string]string{blockedPodLabel: podName},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"statefulset.kubernetes.io/pod-name": podName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				// No peers: any source, on every TCP port but the blocked one.
				Ports: allTCPPortsExcept(int32(port)),
			}},
		},
	}
	t.Logf("Blocking ingress to port %d of pod %q", port, podName)
	require.NoError(t, t.Create(ctx, policy))
	t.Cleanup(func(ctx context.Context) {
		_ = t.Delete(ctx, policy)
	})
}

// podPortShouldBeUnreachable and podPortShouldBeReachable assert, from
// inside the cluster, whether a pod's port can be connected to. Blocking a
// port with a NetworkPolicy is not synchronous -- k3s's policy controller
// programs its rules on a sync loop that has taken minutes under CI load --
// so a test that blocks a port and immediately asserts on the consequence
// is really asserting that the block landed in time. These make that a
// precondition of its own, with its own failure message.
func podPortShouldBeUnreachable(ctx context.Context, t framework.TestingT, port int, podName string) {
	requirePodPortReachability(ctx, t, port, podName, false)
}

func podPortShouldBeReachable(ctx context.Context, t framework.TestingT, port int, podName string) {
	requirePodPortReachability(ctx, t, port, podName, true)
}

func requirePodPortReachability(ctx context.Context, t framework.TestingT, port int, podName string, want bool) {
	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	target, err := kube.Get[corev1.Pod](ctx, ctl, kube.ObjectKey{Namespace: t.Namespace(), Name: podName})
	require.NoErrorf(t, err, "Pod with name %q not found", podName)
	require.NotEmptyf(t, target.Status.PodIP, "Pod %q has no address to connect to", podName)

	// Dial from a sibling pod: a pod reaches itself without leaving the node,
	// which an ingress policy does not govern.
	from := siblingPod(ctx, t, ctl, target)

	state := func(reachable bool) string {
		if reachable {
			return "reachable"
		}
		return "unreachable"
	}
	// curl reports any connect failure as a non-zero exit, which is what
	// "unreachable" means here -- the port is not answering, whatever the
	// reason.
	cmd := fmt.Sprintf("curl -sS -m 2 -o /dev/null http://%s >/dev/null 2>&1 && echo reachable || echo unreachable",
		net.JoinHostPort(target.Status.PodIP, strconv.Itoa(port)))

	t.Logf("Checking port %d of pod %q is %s from pod %q", port, podName, state(want), from.Name)
	var last string
	require.Eventually(t, func() bool {
		var stdout bytes.Buffer
		if err := ctl.Exec(ctx, from, kube.ExecOptions{Command: []string{"sh", "-c", cmd}, Stdout: &stdout}); err != nil {
			t.Logf("exec in pod %q failed: %v", from.Name, err)
			return false
		}
		last = strings.TrimSpace(stdout.String())
		return last == state(want)
	}, 5*time.Minute, 5*time.Second, "%s", delayLog(func() string {
		return fmt.Sprintf("port %d of pod %q never became %s from pod %q (last result: %q)", port, podName, state(want), from.Name, last)
	}))
}

// siblingPod returns another running pod of the same cluster as pod, to dial
// from.
func siblingPod(ctx context.Context, t framework.TestingT, ctl *kube.Ctl, pod *corev1.Pod) *corev1.Pod {
	pods, err := kube.List[corev1.PodList](ctx, ctl, t.Namespace(), client.MatchingLabels{
		"app.kubernetes.io/instance": pod.Labels["app.kubernetes.io/instance"],
		"app.kubernetes.io/name":     pod.Labels["app.kubernetes.io/name"],
	})
	require.NoError(t, err)

	for i := range pods.Items {
		sibling := &pods.Items[i]
		if sibling.Name != pod.Name && sibling.Status.Phase == corev1.PodRunning && sibling.DeletionTimestamp == nil {
			return sibling
		}
	}
	t.Fatalf("no running sibling of pod %q to dial from", pod.Name)
	return nil
}

// allTCPPortsExcept is every TCP port but one, as the ranges a
// NetworkPolicy ingress rule takes.
func allTCPPortsExcept(port int32) []networkingv1.NetworkPolicyPort {
	tcp := ptr.To(corev1.ProtocolTCP)
	var ports []networkingv1.NetworkPolicyPort
	if port > 1 {
		ports = append(ports, networkingv1.NetworkPolicyPort{Protocol: tcp, Port: ptr.To(intstr.FromInt32(1)), EndPort: ptr.To(port - 1)})
	}
	if port < 65535 {
		ports = append(ports, networkingv1.NetworkPolicyPort{Protocol: tcp, Port: ptr.To(intstr.FromInt32(port + 1)), EndPort: ptr.To(int32(65535))})
	}
	return ports
}

func iUnblockIngressToPod(ctx context.Context, t framework.TestingT, podName string) {
	t.Logf("Unblocking ingress to pod %q", podName)
	require.NoError(t, t.DeleteAllOf(ctx, &networkingv1.NetworkPolicy{}, client.InNamespace(t.Namespace()), client.MatchingLabels{blockedPodLabel: podName}))
}

// podShouldBeReady asserts the pod is Ready right now -- not eventually. It
// pins that steering a port never touched pod readiness.
func podShouldBeReady(ctx context.Context, t framework.TestingT, podName string) {
	var pod corev1.Pod
	require.NoError(t, t.Get(ctx, t.ResourceKey(podName), &pod))
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			require.Equal(t, corev1.ConditionTrue, condition.Status, "pod %q is not ready", podName)
			return
		}
	}
	t.Fatalf("pod %q reports no Ready condition", podName)
}
