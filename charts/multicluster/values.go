// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_values.go.tpl
package multicluster

import (
	_ "embed"

	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed values.yaml
	DefaultValuesYAML []byte

	//go:embed values.schema.json
	ValuesSchemaJSON []byte
)

type Values struct {
	Image                        Image                       `json:"image"`
	Node                         string                      `json:"node" jsonschema:"required"`
	KubernetesAPIExternalAddress string                      `json:"apiServerExternalAddress" jsonschema:"required"`
	Peers                        []Peer                      `json:"peers" jsonschema:"required"`
	LogLevel                     string                      `json:"logLevel"`
	Resources                    corev1.ResourceRequirements `json:"resources"`
	PodTemplate                  *PodTemplateSpec            `json:"podTemplate,omitempty"`
	InstallCRDs                  bool                        `json:"installCRDs"`
}

type Peer struct {
	Name    string `json:"name,omitempty" jsonschema:"required"`
	Address string `json:"address,omitempty" jsonschema:"required"`
}

type PodTemplateSpec struct {
	Metadata Metadata       `json:"metadata,omitempty"`
	Spec     corev1.PodSpec `json:"spec,omitempty" jsonschema:"required"`
}

type Metadata struct {
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type Image struct {
	Repository string  `json:"repository"`
	Tag        *string `json:"tag,omitempty"`
}
