package steps

import (
	"bytes"
	"context"
	"strings"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

func runScriptInClusterCheckOutput(ctx context.Context, t framework.TestingT, command string, output *godog.DocString) {
	var redpandas redpandav1alpha2.RedpandaList
	require.NoError(t, t.List(ctx, &redpandas))

	if len(redpandas.Items) != 1 {
		require.FailNow(t, "expected to find 1 %T but found %d", (*redpandav1alpha2.Redpanda)(nil), len(redpandas.Items))
	}

	redpanda := redpandas.Items[0]

	var sts appsv1.StatefulSet
	require.NoError(t, t.Get(ctx, t.ResourceKey(redpanda.Name), &sts))

	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	require.NoError(t, err)

	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, client.MatchingLabelsSelector{
		Selector: selector,
	}))

	if len(pods.Items) < 1 {
		require.FailNow(t, "expected to find at least 1 Pod but found none")
	}

	pod := pods.Items[0]

	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	t.Logf("executing %q in Pod %q", command, pod.Name)

	var stdout bytes.Buffer
	require.NoError(t, ctl.Exec(ctx, &pod, kube.ExecOptions{
		Container: "redpanda",
		Command:   []string{"/bin/bash", "-c", command},
		Stdout:    &stdout,
	}))

	// Correct for extra whitespace from either the command itself or from
	// godog's parsing.
	expected := strings.Trim(output.Content, "\n ")
	actual := strings.Trim(stdout.String(), "\n ")

	require.Equal(t, expected, actual)
}
