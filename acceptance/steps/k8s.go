package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

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
		cluster.SetGroupVersionKind(redpandav1alpha2.GroupVersion.WithKind("Redpanda"))

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
