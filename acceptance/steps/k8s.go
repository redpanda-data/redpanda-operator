package steps

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func statefulSetHaveOwnerReference(ctx context.Context, t framework.TestingT, statefulsetName, clusterName string) {
	var sts appsv1.StatefulSet
	var cluster redpandav1alpha2.Redpanda

	clusterKey := t.ResourceKey(clusterName)
	stsKey := t.ResourceKey(statefulsetName)

	t.Logf("Checking cluster %q set owner reference in statefulset", clusterName)
	require.Eventually(t, func() bool {
		require.NoError(t, t.Get(ctx, clusterKey, &cluster))
		require.NoError(t, t.Get(ctx, stsKey, &sts))
		cluster.SetGroupVersionKind(redpandav1alpha2.GroupVersion.WithKind("Redpanda"))

		if len(sts.OwnerReferences) != 1 {
			t.Logf("Statefulset has %d owner references", len(sts.OwnerReferences))
			return false
		}

		obj := cluster.OwnerShipRefObj()
		obj.UID = types.UID("")
		expected, err := json.Marshal(obj)
		require.NoError(t, err)

		obj = sts.OwnerReferences[0]
		obj.UID = types.UID("")
		actual, err := json.Marshal(obj)
		require.NoError(t, err)

		t.Logf(`Checking cluster StatefulSet contains owner reference? %s/%s -> %s`, sts.OwnerReferences[0].APIVersion, sts.OwnerReferences[0].Kind, sts.OwnerReferences[0].Name)
		return string(expected) == string(actual)
	}, 5*time.Minute, 5*time.Second, `Cluster %q never set owner reference for StatefulSet %q, final OwnerReference: %+v`, clusterKey.String(), stsKey.String(), sts.OwnerReferences)
	t.Logf("StatefulSet has Redpanda %s custom resource OwnerReference", clusterName)
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
