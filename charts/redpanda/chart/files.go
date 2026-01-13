// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package chart

import "embed"

//go:embed Chart.lock
//go:embed Chart.yaml
//go:embed files/*
//go:embed templates/*
//go:embed values.schema.json
//go:embed values.yaml
var ChartFiles embed.FS
