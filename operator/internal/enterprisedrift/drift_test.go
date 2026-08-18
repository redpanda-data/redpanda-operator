// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package enterprisedrift pins the tiny contracts that the enterprise module
// intentionally duplicates from OSS packages it cannot import (the enterprise
// module may only depend on common-go/* and third-party modules; see
// enterprise/lint/boundary_test.go). This package lives in the OSS operator
// module precisely because it may import both worlds. If one of these tests
// fails, the OSS original and its enterprise copy have drifted: update the
// copy in enterprise/operator/render/hosttuner.go or
// enterprise/operator/render/clusterconfig.go to match the original (or vice
// versa, if the change originated on the enterprise side).
package enterprisedrift

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	redpanda "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/enterprise/operator/render"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// TestHostTunerDrift pins each HostTuner* copy in
// enterprise/operator/render/hosttuner.go to its original in
// charts/redpanda/v25 (statefulset.go and values.go). Every symbol is either
// a constant or a pure nullary function, so equality of the produced values
// is a complete drift check.
func TestHostTunerDrift(t *testing.T) {
	require.Equal(t, redpanda.HostTunerStateFilePath, render.HostTunerStateFilePath)
	require.Equal(t, redpanda.HostTunerDirs(), render.HostTunerDirs())
	require.Equal(t, redpanda.HostTunerScript(), render.HostTunerScript())
	require.Equal(t, redpanda.HostTunerDefaults(), render.HostTunerDefaults())
	require.Equal(t, redpanda.HostTunerVolumes(), render.HostTunerVolumes())
	require.Equal(t, redpanda.HostTunerVolumeMounts(), render.HostTunerVolumeMounts())
	require.Equal(t, redpanda.HostTunerStateVolumeMount(), render.HostTunerStateVolumeMount())
}

// TestFixupWireContractDrift pins the Fixup wire-contract copy in
// enterprise/operator/render/clusterconfig.go to pkg/clusterconfiguration's
// original. The enterprise renderer JSON-serializes Fixups into the bootstrap
// configmap and the OSS sidecar deserializes them with the original struct,
// so field names, types, and JSON tags must match exactly.
func TestFixupWireContractDrift(t *testing.T) {
	original := reflect.TypeOf(clusterconfiguration.Fixup{})
	copied := reflect.TypeOf(render.Fixup{})

	require.Equal(t, original.NumField(), copied.NumField())
	for i := 0; i < original.NumField(); i++ {
		origField := original.Field(i)
		copyField := copied.Field(i)
		require.Equal(t, origField.Name, copyField.Name)
		require.Equal(t, origField.Type, copyField.Type)
		require.Equal(t, origField.Tag.Get("json"), copyField.Tag.Get("json"))
	}
}

// TestFixupWireRoundTrip exercises the wire contract itself: marshal the
// enterprise copy, unmarshal with the OSS original, and compare. The
// field-walk above can't see a custom MarshalJSON/UnmarshalJSON added to
// either side — this round-trip fails the moment (de)serialization semantics
// diverge, which is exactly what would corrupt the bootstrap configmap
// handoff between the enterprise renderer and the OSS sidecar.
func TestFixupWireRoundTrip(t *testing.T) {
	entFixup := render.Fixup{
		Field: "redpanda.advertised_kafka_api",
		CEL:   `envString("HOST_IP")`,
	}

	wire, err := json.Marshal(entFixup)
	require.NoError(t, err)

	var ossFixup clusterconfiguration.Fixup
	require.NoError(t, json.Unmarshal(wire, &ossFixup))
	require.Equal(t, entFixup.Field, ossFixup.Field)
	require.Equal(t, entFixup.CEL, ossFixup.CEL)

	back, err := json.Marshal(ossFixup)
	require.NoError(t, err)
	require.JSONEq(t, string(wire), string(back))
}

// TestCELMacroNameDrift pins the CEL macro-name constants duplicated in
// enterprise/operator/render/clusterconfig.go to the originals in
// pkg/clusterconfiguration/cel_macros.go. The enterprise renderer embeds
// these names in the CEL expressions it serializes; the OSS sidecar's CEL
// environment only registers functions under the original names.
func TestCELMacroNameDrift(t *testing.T) {
	require.Equal(t, clusterconfiguration.CELEnvString, render.CELEnvString)
	require.Equal(t, clusterconfiguration.CELRepr, render.CELRepr)
	require.Equal(t, clusterconfiguration.CELExternalSecretRef, render.CELExternalSecretRef)
	require.Equal(t, clusterconfiguration.CELErrorToWarning, render.CELErrorToWarning)
}
