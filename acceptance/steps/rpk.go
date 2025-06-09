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

func checkRPKCommands(ctx context.Context, t framework.TestingT, clusterName string) {
	ctl, err := kube.FromRESTConfig(t.RestConfig())
	require.NoError(t, err)

	key := t.ResourceKey(clusterName)

	var clusterSet appsv1.StatefulSet
	require.NoError(t, t.Get(ctx, key, &clusterSet))

	selector, err := metav1.LabelSelectorAsSelector(clusterSet.Spec.Selector)
	require.NoError(t, err)

	var pods corev1.PodList
	require.NoError(t, t.List(ctx, &pods, client.MatchingLabelsSelector{
		Selector: selector,
	}))

	for _, p := range pods.Items {
		t.Logf("Checking rpk commands on pod %q", p.Name)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		// rpk.yaml is not available in operator v2. The v1 (Cluster custom resource) needs to
		// create rpk profile file to overcome the problem with flux dependency (kubernetes version
		// mismatch between rpk and flux fork).
		//require.NoErrorf(t, ctl.Exec(ctx, &p, kube.ExecOptions{
		//	Container: "redpanda",
		//	Command:   []string{"rpk", "profile", "print"},
		//	Stdin:     nil,
		//	Stdout:    &stdout,
		//	Stderr:    &stderr,
		//}), "\nStdout: %s\nStderr: %s\n", stdout.String(), stderr.String())
		//require.Len(t, stderr.Bytes(), 0)

		require.NoErrorf(t, ctl.Exec(ctx, &p, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"rpk", "redpanda", "admin", "brokers", "list"},
			Stdin:     nil,
			Stdout:    &stdout,
			Stderr:    &stderr,
		}), "\nStdout: %s\nStderr: %s\n", stdout.String(), stderr.String())
		require.Len(t, stderr.Bytes(), 0)

		require.NoErrorf(t, ctl.Exec(ctx, &p, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"rpk", "registry", "schema", "list"},
			Stdin:     nil,
			Stdout:    &stdout,
			Stderr:    &stderr,
		}), "\nStdout: %s\nStderr: %s\n", stdout.String(), stderr.String())
		require.Len(t, stderr.Bytes(), 0)

		require.NoErrorf(t, ctl.Exec(ctx, &p, kube.ExecOptions{
			Container: "redpanda",
			Command:   []string{"rpk", "topic", "list"},
			Stdin:     nil,
			Stdout:    &stdout,
			Stderr:    &stderr,
		}), "\nStdout: %s\nStderr: %s\n", stdout.String(), stderr.String())
		require.Len(t, stderr.Bytes(), 0)
	}
}
