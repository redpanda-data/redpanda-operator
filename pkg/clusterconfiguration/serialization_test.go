// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package clusterconfiguration_test

import (
	"context"
	"testing"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// Ensure that the templated files and fixups will work to provide a round-trip
func TestSerde(t *testing.T) {
	cfg := clusterconfiguration.NewConfig("namespace", nil, nil)
	cfg.Node.Redpanda.ID = ptr.To(3)
	cfg.Node.AddFixup("pandaproxy_client.scram_username", `"user"`)
	cfg.Cluster.SetAdditionalConfiguration("a", "b")
	cfg.Cluster.AddFixup("superusers", clusterconfiguration.CELAppendYamlStringArray+`(it, "a")`)

	templates, err := cfg.Templates()
	require.NoError(t, err)

	// Unpack the resulting template files and run fixups.
	var node clusterconfiguration.Template[*config.RedpandaYaml]
	require.NoError(t, yaml.Unmarshal([]byte(templates[clusterconfiguration.RedpandaYamlTemplateFile]), &node.Content))
	require.NoError(t, yaml.Unmarshal([]byte(templates[clusterconfiguration.RedpandaYamlFixupFile]), &node.Fixups))

	factory := clusterconfiguration.StdLibFactory(context.TODO(), nil, nil)
	require.NoError(t, node.Fixup(factory))

	assert.Equal(t, 3, *node.Content.Redpanda.ID)
	assert.Equal(t, "user", *node.Content.PandaproxyClient.SCRAMUsername)

	var conf clusterconfiguration.Template[map[string]string]
	require.NoError(t, yaml.Unmarshal([]byte(templates[clusterconfiguration.BootstrapTemplateFile]), &conf.Content))
	require.NoError(t, yaml.Unmarshal([]byte(templates[clusterconfiguration.BootstrapFixupFile]), &conf.Fixups))

	require.NoError(t, conf.Fixup(factory))

	assert.Equal(t, "b", conf.Content["a"])
	assert.Equal(t, `["a"]`, conf.Content["superusers"])
}
