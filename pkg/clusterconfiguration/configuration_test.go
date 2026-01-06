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

	"github.com/redpanda-data/common-go/rpadmin"
	rpkcfg "github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// Check that additionalConfiguration gets routed to the right place
func TestRedpandaProperties(t *testing.T) {
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	_ = config.SetAdditionalFlatProperty("a", `b`)
	_ = config.SetAdditionalFlatProperty("redpanda.c", `d`)
	_, err := config.Templates()
	require.NoError(t, err)
	concreteNode, err := config.ReifyNodeConfiguration(context.TODO())
	require.NoError(t, err)
	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), nil)
	require.NoError(t, err)
	assert.Equal(t, "b", concreteNode.Other["a"])
	assert.NotContains(t, concreteCfg, "a")
	assert.Equal(t, "d", concreteCfg["c"])
	assert.NotContains(t, concreteNode.Other, "c")
}

func TestAdditionalFlatProperties(t *testing.T) {
	// String appends rely on CEL support
	config := clusterconfiguration.NewConfig("namespace", nil, nil)

	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.sasl_mechanisms_overrides", `[{"listener": "kafka","sasl_mechanisms": ["SCRAM"]}]`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.kafka_throughput_control", `[{'name': 'first_group','client_id': 'client1'}, {'client_id': 'consumer-\d+'}, {'name': 'catch all'}]`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.cloud_storage_recovery_topic_validation_mode", `check_manifest_existence`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.default_leaders_preference", `"racks:rack1,rack2"`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.kafka_nodelete_topics", `["__consumer_offsets","_schemas"]`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.partition_autobalancing_mode", `continuous`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.raft_learner_recovery_rate", `104857600`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.aggregate_metrics", `true`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.write_caching_default", `disabled`))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.kafka_memory_share_for_fetch", `80.0`))

	// We require a schema to concretize configuration values
	schema := rpadmin.ConfigSchema{
		"sasl_mechanisms_overrides": rpadmin.ConfigPropertyMetadata{
			Type:         "array",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
			Items: rpadmin.ConfigPropertyItems{
				Type: "config::sasl_mechanisms_override",
			},
		},
		"kafka_throughput_control": rpadmin.ConfigPropertyMetadata{
			Type:         "array",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
			Items: rpadmin.ConfigPropertyItems{
				Type: "config::throughput_control_group",
			},
		},
		"cloud_storage_recovery_topic_validation_mode": rpadmin.ConfigPropertyMetadata{
			Type:         "recovery_validation_mode",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
		"default_leaders_preference": rpadmin.ConfigPropertyMetadata{
			Type:         "leaders_preference",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
		"kafka_nodelete_topics": rpadmin.ConfigPropertyMetadata{
			Type:         "array",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
			Items: rpadmin.ConfigPropertyItems{
				Type: "string",
			},
		},
		"partition_autobalancing_mode": rpadmin.ConfigPropertyMetadata{
			Type:         "partition_autobalancing_mode",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
		"raft_learner_recovery_rate": rpadmin.ConfigPropertyMetadata{
			Type:         "integer",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "tunable",
		},
		"aggregate_metrics": rpadmin.ConfigPropertyMetadata{
			Type:         "boolean",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
		"write_caching_default": rpadmin.ConfigPropertyMetadata{
			Type:         "string",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
		"disk_reservation_percent": rpadmin.ConfigPropertyMetadata{
			Type:         "number",
			Nullable:     false,
			NeedsRestart: false,
			IsSecret:     false,
			Visibility:   "user",
		},
	}

	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), schema)
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"listener": "kafka", "sasl_mechanisms": []any{"SCRAM"}}}, concreteCfg["sasl_mechanisms_overrides"])
	assert.Equal(t, []any{map[string]any{"name": "first_group", "client_id": "client1"}, map[string]any{"client_id": "consumer-\\d+"}, map[string]any{"name": "catch all"}}, concreteCfg["kafka_throughput_control"])
	assert.Equal(t, "check_manifest_existence", concreteCfg["cloud_storage_recovery_topic_validation_mode"])
	assert.Equal(t, "racks:rack1,rack2", concreteCfg["default_leaders_preference"])
	assert.Equal(t, []string{"__consumer_offsets", "_schemas"}, concreteCfg["kafka_nodelete_topics"])
	assert.Equal(t, "continuous", concreteCfg["partition_autobalancing_mode"])
	assert.Equal(t, int64(104857600), concreteCfg["raft_learner_recovery_rate"])
	assert.Equal(t, true, concreteCfg["aggregate_metrics"])
	assert.Equal(t, "disabled", concreteCfg["write_caching_default"])
	assert.Equal(t, "80.0", concreteCfg["kafka_memory_share_for_fetch"])
}

func TestFlatProperties(t *testing.T) {
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.a", "b"))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.node_id", "33"))
	concreteNode, err := config.ReifyNodeConfiguration(context.TODO())
	require.NoError(t, err)
	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), nil)
	require.NoError(t, err)
	assert.Equal(t, 33, *concreteNode.Redpanda.ID)
	assert.Equal(t, "b", concreteCfg["a"])
	assert.NotContains(t, concreteNode.Redpanda.Other, "a")
}

func TestKnownNodeProperties(t *testing.T) {
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.cloud_storage_cache_directory", "/tmp"))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.rpc_server.port", "8080"))
	require.NoError(t, config.SetAdditionalFlatProperty("redpanda.cloud_storage_region", "us-west-1"))

	concreteNode, err := config.ReifyNodeConfiguration(context.TODO())
	require.NoError(t, err)
	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), nil)
	require.NoError(t, err)

	assert.Equal(t, "/tmp", concreteNode.Redpanda.CloudStorageCacheDirectory)
	assert.Equal(t, 8080, concreteNode.Redpanda.RPCServer.Port)
	assert.Len(t, concreteCfg, 1)
	assert.Equal(t, "us-west-1", concreteCfg["cloud_storage_region"])
}

func TestDeleteProperties(t *testing.T) {
	// There isn't much in the way of deletion, but support dropping properties by
	// supplying "empty" clusterConfiguration entries.
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	config.Cluster.SetAdditionalConfiguration("a1", "x")
	config.Cluster.SetAdditionalConfiguration("a2", "y")
	config.Cluster.Set("a1", clusterconfiguration.ClusterConfigValue{})

	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), nil)
	require.NoError(t, err)

	assert.Len(t, concreteCfg, 1)
	assert.Equal(t, "y", concreteCfg["a2"])
}

func TestStringSliceProperties(t *testing.T) {
	// String appends rely on CEL support
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	config.Cluster.AddFixup("superusers", clusterconfiguration.CELAppendYamlStringArray+`(it, "a")`)
	config.Cluster.AddFixup("superusers", clusterconfiguration.CELAppendYamlStringArray+`(it, "b")`)
	config.Cluster.AddFixup("superusers", clusterconfiguration.CELAppendYamlStringArray+`(it, "c")`)

	// We require a schema to concretize configuration values
	schema := rpadmin.ConfigSchema{"superusers": rpadmin.ConfigPropertyMetadata{
		Type: "array",
		Items: rpadmin.ConfigPropertyItems{
			Type: "string",
		},
	}}

	concreteCfg, err := config.ReifyClusterConfiguration(context.TODO(), schema)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, concreteCfg["superusers"])

	// Can't append to a non-array
	config = clusterconfiguration.NewConfig("namespace", nil, nil)
	config.Cluster.Set("superusers", clusterconfiguration.ClusterConfigValue{Repr: ptr.To(clusterconfiguration.YAMLRepresentation(`"nonarrray"`))})
	config.Cluster.AddFixup("superusers", clusterconfiguration.CELAppendYamlStringArray+`(it, "a")`)
	_, err = config.ReifyClusterConfiguration(context.TODO(), schema)
	assert.Error(t, err)
}

func TestHash_FieldsWithNoHashChange(t *testing.T) {
	config := clusterconfiguration.NewConfig("namespace", nil, nil)
	config.Node.Redpanda.SeedServers = []rpkcfg.SeedServer{}
	nodeConfHash, err := config.GetNodeConfigHash(context.TODO())
	require.NoError(t, err)

	config = clusterconfiguration.NewConfig("namespace", nil, nil)
	config.Node.Redpanda.SeedServers = []rpkcfg.SeedServer{{Host: rpkcfg.SocketAddress{Address: "redpanda.com", Port: 9090}}}
	nodeConfHashNew, err := config.GetNodeConfigHash(context.TODO())
	require.NoError(t, err)

	// seed servers should not affect the hash, so rolling restarts do not take place,
	// e.g., when scaling out/in a cluster.
	// Note: The original comment here also claimed that pandaproxy clients, and schema registry clients
	// should also have a similar effect, but AFAICT that's never been true.
	// If we want that behaviour then we need to update the `removeFieldsThatShouldNotTriggerRestart`
	// method.
	require.Equal(t, nodeConfHash, nodeConfHashNew, "node conf")
}
