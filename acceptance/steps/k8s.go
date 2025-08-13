package steps

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// this is a nasty hack due to the fact that we can't disable the linter for typecheck
// that reports sigs.k8s.io/controller-runtime/pkg/client as unused when it's solely used
// for type assertions
var _ client.Object = (client.Object)(nil)

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

func iExecInPodMatching(
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

	pods, err := kube.List[corev1.PodList](ctx, ctl, client.MatchingLabelsSelector{Selector: selector}, client.InNamespace(t.Namespace()))
	require.NoError(t, err)

	require.True(t, len(pods.Items) > 0, "selector %q found no Pods", selector.String())

	var stdout bytes.Buffer
	require.NoError(t, ctl.Exec(ctx, &pods.Items[0], kube.ExecOptions{
		Command: []string{"sh", "-c", cmd},
		Stdout:  &stdout,
	}))

	assert.Equal(t, expected.Content, stdout.String())
}
