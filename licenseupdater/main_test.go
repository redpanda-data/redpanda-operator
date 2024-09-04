// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"os"
	"testing"
)

func TestMain(t *testing.T) {
	writer = &fsWriter{
		suffix: ".golden",
		write:  os.Getenv("REGENERATE_GOLDEN_FILES") == "true",
		differ: diffChecker(),
	}
	licenseTemplateData = &templateData{
		Year: 9999, // pin the year to make sure our tests don't randomly start failing at a year switch
	}

	config := &config{
		Path:             "testdata",
		LicenseDirectory: "testdata/licenses",
		Licenses:         []string{"BSL", "MIT", "RCL"},
		Matches: []*match{
			{
				Extension: ".go",
				Type:      "go",
				License:   "BSL",
			},
			{
				Extension: ".yaml",
				Match:     "helm",
				Type:      "helm",
				License:   "MIT",
			},
			{
				Extension: ".yaml",
				Type:      "yaml",
				License:   "RCL",
			},
		},
	}

	if err := config.initializeAndValidate(); err != nil {
		t.Fatal("unexpected error", err)
	}

	if err := doMain(config); err != nil {
		t.Fatal("unexpected error", err)
	}

	if err := writer.differ.error(); err != nil {
		t.Fatal("unexpected error", err)
	}
}
