// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// Config is .golangci.yml: golangci-lint's own format, read by this driver
// instead of by golangci-lint. Only the structure is modelled; each tool
// decodes its own settings block through Settings, so adding a tool never
// touches this file. Keys not modelled are ignored, which keeps the file valid
// input for a golangci-lint build carrying the same custom linters.
type Config struct {
	Linters struct {
		Default    string                     `json:"default"`
		Enable     []string                   `json:"enable"`
		Settings   map[string]json.RawMessage `json:"settings"`
		Exclusions struct {
			Paths []string `json:"paths"`
			Rules []struct {
				Path    string   `json:"path"`
				Text    string   `json:"text"`
				Linters []string `json:"linters"`
			} `json:"rules"`
		} `json:"exclusions"`
	} `json:"linters"`
	Formatters struct {
		Enable   []string                   `json:"enable"`
		Settings map[string]json.RawMessage `json:"settings"`
	} `json:"formatters"`

	enabled    map[string]bool
	exclusions []exclusion
}

// load reads the file found walking up from the package being analysed, or
// from HOUSELINT_CONFIG. It returns nil for a process that must not need one:
// go vet's -V=full and -flags calls, and its facts-only runs on dependencies,
// which live in the module cache far from any config. nil means everything
// runs with default settings, which is what producing facts needs and what
// -flags describes; nothing is reported in those runs anyway.
func load() *Config {
	if len(os.Args) == 2 && slices.Contains([]string{"-V=full", "-flags", "-h", "-help"}, os.Args[1]) {
		return nil
	}

	if unit().VetxOnly {
		return nil
	}

	path := os.Getenv("HOUSELINT_CONFIG")
	if path == "" {
		if path = find(); path == "" {
			fatalf("no .golangci.yml found above %s; set HOUSELINT_CONFIG to point at one", startDir())
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fatalf("%v", err)
	}

	c := &Config{enabled: map[string]bool{}}
	if err := yaml.Unmarshal(data, c); err != nil {
		fatalf("%s: %v", path, err)
	}

	if c.Linters.Default != "" && c.Linters.Default != "none" {
		fatalf("%s: linters.default: %q is not supported; list the linters under linters.enable", path, c.Linters.Default)
	}

	for _, name := range slices.Concat(c.Linters.Enable, c.Formatters.Enable) {
		c.enabled[name] = true
	}

	for _, p := range c.Linters.Exclusions.Paths {
		c.exclusions = append(c.exclusions, exclusion{path: regexp.MustCompile(p)})
	}

	for _, r := range c.Linters.Exclusions.Rules {
		e := exclusion{linters: r.Linters}
		if r.Path != "" {
			e.path = regexp.MustCompile(r.Path)
		}

		if r.Text != "" {
			e.text = regexp.MustCompile(r.Text)
		}

		c.exclusions = append(c.exclusions, e)
	}

	return c
}

// check rejects an enabled name the registry does not have, which is what
// golangci-lint does for a linter it does not know.
func (c *Config) check(registry []linter) {
	var names []string
	for _, l := range registry {
		names = append(names, l.name)
	}

	for name := range c.enabled {
		if !slices.Contains(names, name) {
			slices.Sort(names)
			fatalf("%q is not in the registry (%s); see lint/main.go", name, strings.Join(names, ", "))
		}
	}
}

// Settings decodes a tool's block into v, its own struct json-tagged with
// golangci-lint's keys. linters.settings.<name> and formatters.settings.<name>
// are tried, then linters.settings.custom.<name>.settings, where golangci-lint
// keeps a module plugin's. No block, or no config, leaves v as it was.
func (c *Config) Settings(name string, v any) error {
	if c == nil {
		return nil
	}

	for _, settings := range []map[string]json.RawMessage{c.Linters.Settings, c.Formatters.Settings} {
		if raw, ok := settings[name]; ok {
			return json.Unmarshal(raw, v)
		}
	}

	var custom map[string]struct {
		Settings json.RawMessage `json:"settings"`
	}
	if raw, ok := c.Linters.Settings["custom"]; ok {
		if err := json.Unmarshal(raw, &custom); err != nil {
			return fmt.Errorf("linters.settings.custom: %w", err)
		}
	}

	if entry, ok := custom[name]; ok {
		return json.Unmarshal(entry.Settings, v)
	}

	return nil
}

// find walks up from startDir looking for the file.
func find() string {
	for dir := startDir(); ; dir = filepath.Dir(dir) {
		for _, name := range []string{".golangci.yml", ".golangci.yaml"} {
			if path := filepath.Join(dir, name); exists(path) {
				return path
			}
		}

		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// startDir is the directory of the package being analysed when go vet is the
// caller, else the working directory.
func startDir() string {
	if u := unit(); u.Dir != "" {
		return u.Dir
	}

	wd, _ := os.Getwd()

	return wd
}

// unit is the unit config go vet hands the tool, as `-json <file>.cfg`, or
// the zero value when this is not such a run.
func unit() (u struct {
	Dir      string
	VetxOnly bool
},
) {
	for _, arg := range os.Args[1:] {
		if strings.HasSuffix(arg, ".cfg") {
			if data, err := os.ReadFile(arg); err == nil {
				_ = json.Unmarshal(data, &u)
			}
		}
	}

	return u
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "houselint: "+format+"\n", args...)
	os.Exit(2)
}

// settings decodes a tool's block into v or exits: a malformed block is a
// configuration error, not a finding.
func settings(name string, v any) {
	if err := cfg.Settings(name, v); err != nil {
		fatalf("settings for %s: %v", name, err)
	}
}
