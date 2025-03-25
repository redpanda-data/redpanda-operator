// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package configuration provide tools for the operator to manage cluster configuration
package configuration

import (
	"context"
	"crypto/md5" //nolint:gosec // this is not encrypting secure info
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

const (
	// useMixedConfiguration can be temporarily used until .boostrap.yaml is fully supported.
	useMixedConfiguration = true

	redpandaPropertyPrefix = "redpanda."
)

// knownNodeProperties is initialized with all well known node properties from the "redpanda" configuration tree.
var knownNodeProperties map[string]bool

// GlobalConfiguration is a configuration object that holds both node/local configuration and global configuration.
type GlobalConfiguration struct {
	NodeConfiguration      *config.RedpandaYaml
	ClusterConfiguration   map[string]interface{}
	BootstrapConfiguration map[string]vectorizedv1alpha1.ClusterConfigValue
	Mode                   GlobalConfigurationMode
	// The bootstrap will be expanded once in-process into this
	concreteValues map[string]any
}

// For constructs a GlobalConfiguration for the given version of the cluster (considering feature gates).
func For(_ string) *GlobalConfiguration {
	return &GlobalConfiguration{
		Mode:              DefaultCentralizedMode(),
		NodeConfiguration: config.ProdDefault(),
	}
}

// DefaultCentralizedMode determines the default strategy to use when centralized configuration is enabled
func DefaultCentralizedMode() GlobalConfigurationMode {
	// Use mixed config temporarily
	if useMixedConfiguration {
		return GlobalConfigurationModeMixed
	}
	return GlobalConfigurationModeCentralized
}

// SetAdditionalRedpandaProperty allows setting an unstructured redpanda property on the configuration.
// Depending on the mode, it will be put in node configuration, centralized configuration, or both.
func (c *GlobalConfiguration) SetAdditionalRedpandaProperty(
	key string, value interface{},
) {
	c.Mode.SetAdditionalRedpandaProperty(c, key, value)
}

// AppendToAdditionalRedpandaProperty allows appending values to string slices in additional redpanda properties.
//
//nolint:goerr113 // no need to define static error
func (c *GlobalConfiguration) AppendToAdditionalRedpandaProperty(
	key string, value string,
) error {
	val := c.GetAdditionalRedpandaProperty(key)
	valAsSlice, ok := val.([]string)
	if !ok && val != nil {
		return fmt.Errorf("property %q is not a string slice: %v", key, val)
	}
	valAsSlice = append(valAsSlice, value)
	c.SetAdditionalRedpandaProperty(key, valAsSlice)
	return nil
}

// SetAdditionalFlatProperties allows setting additional properties on the configuration from a key/value configuration format.
// Properties will be put in the right bucket (node and/or cluster) depending on the configuration mode.
func (c *GlobalConfiguration) SetAdditionalFlatProperties(
	props map[string]string,
) error {
	return c.Mode.SetAdditionalFlatProperties(c, props)
}

// ConcreteConfiguration uses the expander to completely bottom out all configuration values
// into concrete ones, according to the supplied schema. This is performed once only,
// with repeated calls using the cached computation.
func (c *GlobalConfiguration) ConcreteConfiguration(ctx context.Context, reader client.Reader, cloudExpander *pkgsecrets.CloudExpander, namespace string, schema rpadmin.ConfigSchema) (map[string]any, error) {
	if c.concreteValues != nil {
		return c.concreteValues, nil
	}
	concreteCfg, err := clusterconfiguration.ExpandForConfiguration(ctx, reader, cloudExpander, namespace, c.BootstrapConfiguration, schema)
	if err != nil {
		return nil, err
	}

	c.concreteValues = concreteCfg
	return c.concreteValues, nil
}

// FinalizeToTemplate takes any accumulated values and marshals them into a format
// that can be utilised by the bootstrap templating machinery.
// This is only called once, near the end of the construction of a GlobalConfiguration,
// but we expose the function for the use by a handful of unit tests.
// (We currently preserve the ClusterConfiguration attribute solely to support
// the accumulation of array values, which is used to manage some attributes
// during the assembly of a configuration; without this we'd need to repeatedly
// round-trip into and out of a serialised form.)
// TODO: refactor this.
func (c *GlobalConfiguration) FinalizeToTemplate() error {
	if c.BootstrapConfiguration == nil {
		c.BootstrapConfiguration = make(map[string]vectorizedv1alpha1.ClusterConfigValue)
	}
	for k, v := range c.ClusterConfiguration {
		if _, found := c.BootstrapConfiguration[k]; found {
			continue
		}
		// These values are all "concrete" - that is, they're not looked up anywhere.
		// We use JSON marshalling as opposed to YAML here - it has fewer options available, but is
		// compatible for use in either a YAML or JSON target document.
		buf, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("cannot marshal concrete cluster configuration value: %w", err)
		}
		// These values are all stringified, so that they can be written into the template file.
		c.BootstrapConfiguration[k] = vectorizedv1alpha1.ClusterConfigValue{
			Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation(buf)),
		}
	}
	return nil
}

// GetCentralizedConfigurationHash computes a hash of the centralized configuration considering only the
// cluster properties that require a restart (this is why the schema is needed).
func (c *GlobalConfiguration) GetCentralizedConfigurationHash(
	ctx context.Context,
	reader client.Reader,
	cloudExpander *pkgsecrets.CloudExpander,
	schema rpadmin.ConfigSchema,
	namespace string,
) (string, error) {
	concreteCfg, err := c.ConcreteConfiguration(ctx, reader, cloudExpander, namespace, schema)
	if err != nil {
		return "", err
	}

	return c.GetCentralizedConcreteConfigurationHash(concreteCfg, schema)
}

// GetCentralizedConcreteConfigurationHash is a short-term helper to handle checking of cluster configuration.
// It hashes only properties that require a restart.
func (c *GlobalConfiguration) GetCentralizedConcreteConfigurationHash(
	concreteCfg map[string]any,
	schema rpadmin.ConfigSchema,
) (string, error) {
	clone := *c

	// Ignore cluster properties that don't require restart
	clone.ClusterConfiguration = make(map[string]any)
	for k, v := range concreteCfg {
		// Unknown properties should be ignored as they might be user errors
		if meta, ok := schema[k]; ok && meta.NeedsRestart {
			clone.ClusterConfiguration[k] = v
		}
	}
	serialized, err := clone.Serialize()
	if err != nil {
		return "", err
	}
	// We keep using md5 for having the same format as node hash
	md5Hash := md5.Sum(serialized.BootstrapFile) //nolint:gosec // this is not encrypting secure info
	return fmt.Sprintf("%x", md5Hash), nil
}

// GetNodeConfigurationHash computes a hash of the node configuration considering only node properties
// but excluding fields that trigger unnecessary restarts.
func (c *GlobalConfiguration) GetNodeConfigurationHash() (string, error) {
	clone := *c
	// clean any cluster property from config before serializing
	clone.ClusterConfiguration = nil
	removeFieldsThatShouldNotTriggerRestart(&clone)
	props := clone.NodeConfiguration.Redpanda.Other
	clone.NodeConfiguration.Redpanda.Other = make(map[string]interface{})
	for k, v := range props {
		if isKnownNodeProperty(fmt.Sprintf("%s%s", redpandaPropertyPrefix, k)) {
			clone.NodeConfiguration.Redpanda.Other[k] = v
		}
	}
	serialized, err := clone.Serialize()
	if err != nil {
		return "", err
	}
	md5Hash := md5.Sum(serialized.RedpandaFile) //nolint:gosec // this is not encrypting secure info
	return fmt.Sprintf("%x", md5Hash), nil
}

// GetFullConfigurationHash computes the hash of the full configuration, i.e., the plain
// "redpanda.yaml" file, with the exception of fields that trigger unnecessary restarts.
func (c *GlobalConfiguration) GetFullConfigurationHash() (string, error) {
	clone := *c
	removeFieldsThatShouldNotTriggerRestart(&clone)
	serialized, err := clone.Serialize()
	if err != nil {
		return "", err
	}
	md5Hash := md5.Sum(serialized.RedpandaFile) //nolint:gosec // this is not encoding secure info
	return fmt.Sprintf("%x", md5Hash), nil
}

// Ignore seeds in the hash computation such that any seed changes do not
// trigger a rolling restart across the nodes.
func removeFieldsThatShouldNotTriggerRestart(c *GlobalConfiguration) {
	c.NodeConfiguration.Redpanda.SeedServers = []config.SeedServer{}
}

// GetAdditionalRedpandaProperty retrieves a configuration option
func (c *GlobalConfiguration) GetAdditionalRedpandaProperty(
	prop string,
) interface{} {
	return c.Mode.GetAdditionalRedpandaProperty(c, prop)
}

func isKnownNodeProperty(prop string) bool {
	if v, ok := knownNodeProperties[prop]; ok {
		return v
	}
	for k := range knownNodeProperties {
		if strings.HasPrefix(prop, fmt.Sprintf("%s.", k)) {
			return true
		}
	}
	return false
}

func init() {
	knownNodeProperties = make(map[string]bool)

	// The assumption here is that all explicit fields of RedpandaNodeConfig are node properties
	cfg := reflect.TypeOf(config.RedpandaNodeConfig{})
	for i := 0; i < cfg.NumField(); i++ {
		tag := cfg.Field(i).Tag
		yamlTag := tag.Get("yaml")
		parts := strings.Split(yamlTag, ",")
		if len(parts) > 0 && parts[0] != "" {
			knownNodeProperties[fmt.Sprintf("redpanda.%s", parts[0])] = true
		}
	}
}
