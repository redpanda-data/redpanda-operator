package steps

import (
	"context"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
	"github.com/redpanda-data/redpanda-operator/pkg/helm"
)

func iInstallHelmRelease(ctx context.Context, t framework.TestingT, name string, values *godog.DocString) {
	var valuesMap map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(values.Content), &valuesMap))

	t.InstallLocalHelmChart(ctx, "../charts/redpanda", helm.InstallOptions{
		Name:      name,
		Namespace: t.Namespace(),
		Values:    valuesMap,
	}, helm.Dependency{Name: "redpanda", Repository: "https://charts.redpanda.com"})
}
