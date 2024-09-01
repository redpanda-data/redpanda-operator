package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func TestMain(t *testing.T) {
	testWriter := &testWriter{
		suffix: ".golden",
		data:   make(map[string][]byte),
		write:  os.Getenv("REGENERATE_GOLDEN_FILES") == "true",
	}

	writer = testWriter
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

	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal("unexpected error", err)
	}

	dmp := diffmatchpatch.New()
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".golden") {
			continue
		}

		path := "testdata/" + entry.Name()

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("unexpected error", err)
		}

		actual := data
		expected := testWriter.data[path]
		if !bytes.Equal(actual, expected) {
			diffs := dmp.DiffMain(string(actual), string(expected), false)

			t.Fatalf("expected golden file to match:\n %s", dmp.DiffPrettyText(diffs))
		}
	}

	entries, err = os.ReadDir("testdata/licenses")
	if err != nil {
		t.Fatal("unexpected error", err)
	}

	for _, license := range []string{
		"BSL", "MIT", "RCL",
	} {
		found := false
		for _, entry := range entries {
			if entry.Name() == license+".md.golden" {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("license %q not found", license)
		}
	}
}
