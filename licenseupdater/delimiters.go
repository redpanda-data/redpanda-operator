// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

type delimiter struct {
	Top    string `yaml:"top"`
	Middle string `yaml:"middle"`
	Bottom string `yaml:"bottom"`
}

var (
	emptyDelimiter    = delimiter{}
	builtinDelimiters = map[string]delimiter{
		"go":   {Middle: "//"},
		"yaml": {Middle: "#"},
		"helm": {Top: "{{/*", Bottom: "*/}}"},
	}
)
