package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

func iHelmInstall(ctx context.Context, t framework.TestingT, name, chart, version string, values *godog.DocString) {
	// We don't really reference anything other than the redpanda repo, so just
	// handle repos as a naive check here.
	if strings.HasPrefix(chart, "redpanda/") {
		t.AddHelmRepo(ctx, "redpanda", "https://charts.redpanda.com")
	}

	var valuesMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(values.Content), &valuesMap))

	t.InstallHelmChart(ctx, chart, helm.InstallOptions{
		Name:      name,
		Version:   version,
		Values:    valuesMap,
		Namespace: t.Namespace(),
	})
}

func iHelmUpgrade(ctx context.Context, t framework.TestingT, name, chart, version string, values *godog.DocString) {
	var valuesMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(values.Content), &valuesMap))

	t.UpgradeHelmChart(ctx, name, chart, helm.UpgradeOptions{
		Version:   version,
		Values:    valuesMap,
		Namespace: t.Namespace(),
	})
}

func iDeleteHelmReleaseSecret(ctx context.Context, t framework.TestingT, helmReleaseName string) {
	require.NoError(t, t.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("sh.helm.release.v1.%s.v1", helmReleaseName),
			Namespace: t.Namespace(),
		},
	}))
}
