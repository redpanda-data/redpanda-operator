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
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// helmStoredManifestSecret returns the named Secret as re-rendered into the
// stored manifest of the release's currently deployed revision.
func helmStoredManifestSecret(ctx context.Context, t framework.TestingT, release, secretName string) *corev1.Secret {
	var storageSecrets corev1.SecretList
	require.NoError(t, t.List(ctx, &storageSecrets, client.InNamespace(t.Namespace()), client.MatchingLabels{
		"owner":  "helm",
		"name":   release,
		"status": "deployed",
	}))
	require.Lenf(t, storageSecrets.Items, 1, "expected exactly one deployed release storage secret for %q", release)

	// Helm stores each revision as base64(gzip(json)) under the "release" key.
	payload, err := base64.StdEncoding.DecodeString(string(storageSecrets.Items[0].Data["release"]))
	require.NoError(t, err)
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)

	var stored struct {
		Manifest string `json:"manifest"`
	}
	require.NoError(t, json.Unmarshal(decoded, &stored))

	for _, doc := range strings.Split(stored.Manifest, "\n---") {
		var secret corev1.Secret
		if err := yaml.Unmarshal([]byte(doc), &secret); err != nil {
			continue
		}
		if secret.Kind == "Secret" && secret.Name == secretName {
			return &secret
		}
	}
	require.Failf(t, "secret not found", "Secret %q is not part of the stored manifest of release %q", secretName, release)
	return nil
}

func helmRerendersSecretWithoutServerSetMetadata(ctx context.Context, t framework.TestingT, release, secretName string) {
	secret := helmStoredManifestSecret(ctx, t, release, secretName)

	// Server-populated metadata must not round-trip from the chart's
	// bootstrap user lookup into the rendered manifest: Helm 4 server-side
	// applies rendered manifests and the API server rejects objects carrying
	// managedFields, while uid and resourceVersion act as apply
	// preconditions. See https://github.com/redpanda-data/redpanda-operator/issues/1648.
	require.Empty(t, secret.ManagedFields)
	require.Empty(t, secret.UID)
	require.Empty(t, secret.ResourceVersion)
}

func helmRerendersSecretWithLivePassword(ctx context.Context, t framework.TestingT, release, secretName string) {
	secret := helmStoredManifestSecret(ctx, t, release, secretName)

	var live corev1.Secret
	require.NoError(t, t.Get(ctx, t.ResourceKey(secretName), &live))

	// The re-render must carry the password already in use — anything else
	// means the upgrade regenerated it.
	require.NotEmpty(t, live.Data["password"])
	require.Equal(t, live.Data["password"], secret.Data["password"])
}
