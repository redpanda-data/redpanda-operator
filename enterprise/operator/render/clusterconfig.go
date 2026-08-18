// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package render

// This file mirrors the wire contract consumed by the OSS
// cluster-configuration sidecar (github.com/redpanda-data/redpanda-operator/
// pkg/clusterconfiguration: cel_patcher.go and cel_macros.go). Fixups are
// JSON-serialized into the bootstrap configmap by this renderer and evaluated
// by the sidecar at runtime, so the struct's JSON tags and the CEL macro
// names below must match that package exactly. The enterprise module must
// not import OSS monorepo modules, so the contract is duplicated here; the
// OSS drift-guard test (operator/internal/enterprisedrift) pins each copy to
// its original.

// Fixup is a CEL expression applied to a single cluster-configuration field.
type Fixup struct {
	Field string `json:"field"`
	CEL   string `json:"cel"`
}

// These are provided in case any external code wants to construct CEL expressions.
const (
	CELEnvString         = "envString"
	CELRepr              = "repr"
	CELExternalSecretRef = "externalSecretRef"
	CELErrorToWarning    = "errorToWarning"
)
