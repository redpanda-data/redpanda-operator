// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configuration

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
)

// SerializedGlobalConfigurationContainer wraps the serialized version of redpanda.yaml and .bootstrap.yaml
type SerializedGlobalConfigurationContainer struct {
	RedpandaFile        []byte
	BootstrapFile       []byte
	BootstrapTemplate   []byte
	TemplateEnvironment []corev1.EnvVar
}

// Serialize returns the serialized version of the given configuration
func (c *GlobalConfiguration) Serialize() (
	*SerializedGlobalConfigurationContainer,
	error,
) {
	res := SerializedGlobalConfigurationContainer{}

	rpConfig, err := yaml.Marshal(c.NodeConfiguration)
	if err != nil {
		return nil, fmt.Errorf("could not serialize node config: %w", err)
	}
	res.RedpandaFile = rpConfig

	if len(c.ClusterConfiguration) > 0 {
		clusterConfig, err := yaml.Marshal(c.ClusterConfiguration)
		if err != nil {
			return nil, fmt.Errorf("could not serialize cluster config: %w", err)
		}
		res.BootstrapFile = clusterConfig
	}

	if len(c.BootstrapConfiguration) > 0 {
		readyForTemplate, env, err := clusterconfiguration.ExpandForBootstrap(c.BootstrapConfiguration)
		if err != nil {
			return nil, fmt.Errorf("could not pre-expand cluster bootstrap template: %w", err)
		}
		bootstrapTemplate, err := json.Marshal(readyForTemplate)
		if err != nil {
			return nil, fmt.Errorf("could not serialize cluster bootstrap template: %w", err)
		}
		res.BootstrapTemplate = bootstrapTemplate
		res.TemplateEnvironment = env
	}
	return &res, nil
}

// Deserialize reconstructs a configuration from the given serialized form
func (s *SerializedGlobalConfigurationContainer) Deserialize(
	mode GlobalConfigurationMode,
) (*GlobalConfiguration, error) {
	res := GlobalConfiguration{}
	if s.RedpandaFile != nil {
		if err := yaml.Unmarshal(s.RedpandaFile, &res.NodeConfiguration); err != nil {
			return nil, fmt.Errorf("could not deserialize node config: %w", err)
		}
	}
	if s.BootstrapFile != nil {
		if err := yaml.Unmarshal(s.BootstrapFile, &res.ClusterConfiguration); err != nil {
			return nil, fmt.Errorf("could not deserialize cluster config: %w", err)
		}
	}
	if s.BootstrapTemplate != nil {
		if err := json.Unmarshal(s.BootstrapTemplate, &res.BootstrapConfiguration); err != nil {
			return nil, fmt.Errorf("could not deserialize bootstrap template: %w", err)
		}
	}
	res.Mode = mode // mode is not serialized
	return &res, nil
}
