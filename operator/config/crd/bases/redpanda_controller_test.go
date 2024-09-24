// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/redpanda-data/redpanda-operator/operator/internal/controller/redpanda"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed cluster.redpanda.com_redpandas.yaml
var redpandaCRD []byte

func TestDefluxedMinimumVersion(t *testing.T) {
	crd := v1.CustomResourceDefinition{}

	red, err := yaml.ToJSON(redpandaCRD)
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(red, &crd))
	// 0 is v1alpha1 and 1 is v1alpha2
	recursiveProperties := crd.Spec.Versions[1].Schema.OpenAPIV3Schema.Properties

	require.Contains(t, recursiveProperties["spec"].Properties["chartRef"].Properties["useFlux"].Description, redpanda.HelmChartConstraint)
}
