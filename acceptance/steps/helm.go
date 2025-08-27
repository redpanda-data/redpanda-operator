package steps

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

// The unused parameter is meant to specify a Helm chart place (remote or local in the file system).
func iInstallHelmRelease(ctx context.Context, t framework.TestingT, helmReleaseName, _ string, values *godog.DocString) {
	var valuesMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(values.Content), &valuesMap))

	helmClient, err := helm.New(helm.Options{
		KubeConfig: rest.CopyConfig(t.RestConfig()),
	})
	require.NoError(t, err)

	require.NoError(t, helmClient.RepoAdd(ctx, "console", "https://charts.redpanda.com"))

	path := "../charts/redpanda/chart"
	require.NoError(t, helmClient.DependencyBuild(ctx, path))

	t.Logf("installing chart %q", path)
	_, err = helmClient.Install(ctx, path, helm.InstallOptions{
		Name:      helmReleaseName,
		Namespace: t.Namespace(),
		Values:    valuesMap,
	})
	require.NoError(t, err)
}

func iDeleteHelmReleaseSecret(ctx context.Context, t framework.TestingT, helmReleaseName string) {
	require.NoError(t, t.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("sh.helm.release.v1.%s.v1", helmReleaseName),
			Namespace: t.Namespace(),
		},
	}))
}
