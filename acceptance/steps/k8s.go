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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// this is a nasty hack due to the fact that we can't disable the linter for typecheck
// that reports sigs.k8s.io/controller-runtime/pkg/client as unused when it's solely used
// for type assertions
var _ client.Object = client.Object(nil)

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

// podIsReady asserts the pod is Ready right now -- not eventually. It pins
// that steering a port never touched pod readiness.
func podIsReady(ctx context.Context, t framework.TestingT, podName string) {
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

// operatorLogMatchLimit caps how many matching operator log lines are
// reported, keeping the newest.
const operatorLogMatchLimit = 300

// dumpOperatorLogsMatching reports the shared operator's log lines that
// mention needle. The per-feature diagnostics already dump the tail of that
// log, but one busy reconciler can fill it in a second, so anything about a
// specific resource has to be selected by name out of the whole log.
func dumpOperatorLogsMatching(ctx context.Context, t framework.TestingT, needle string) {
	var pods corev1.PodList
	if err := t.List(ctx, &pods, client.InNamespace(OperatorNamespace)); err != nil {
		t.Logf("[operator-logs] listing pods in %q: %v", OperatorNamespace, err)
		return
	}

	clientset, err := kubernetes.NewForConfig(t.RestConfig())
	if err != nil {
		t.Logf("[operator-logs] building clientset: %v", err)
		return
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, container := range pod.Spec.Containers {
			stream, err := clientset.CoreV1().Pods(OperatorNamespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
			}).Stream(ctx)
			if err != nil {
				t.Logf("[operator-logs] streaming %s/%s: %v", pod.Name, container.Name, err)
				continue
			}
			matches := matchingLines(stream, needle)
			_ = stream.Close()

			if len(matches) == 0 {
				t.Logf("[operator-logs] %s/%s said nothing about %q", pod.Name, container.Name, needle)
				continue
			}
			t.Logf("[operator-logs] %s/%s on %q (%d lines):\n%s", pod.Name, container.Name, needle, len(matches), strings.Join(matches, "\n"))
		}
	}
}

// matchingLines returns the lines of r containing needle, at most
// operatorLogMatchLimit of them, keeping the newest.
func matchingLines(r io.Reader, needle string) []string {
	var matches []string
	scanner := bufio.NewScanner(r)
	// Structured log lines carrying a stack trace run well past the default
	// 64KiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if line := scanner.Text(); strings.Contains(line, needle) {
			matches = append(matches, line)
			if len(matches) > operatorLogMatchLimit {
				matches = matches[1:]
			}
		}
	}
	return matches
}
