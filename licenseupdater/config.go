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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

type staticFile struct {
	Name      string    `yaml:"name"`
	License   string    `yaml:"license"`
	Type      string    `yaml:"type"`
	Delimiter delimiter `yaml:"delimiter"`
}

func (m *staticFile) initializeAndValidate() error {
	var errs []error

	if m.Type != "" {
		if _, ok := builtinDelimiters[m.Type]; !ok {
			errs = append(errs, fmt.Errorf("invalid builtin delimiter type: %q", m.Type))
		}
	}

	if m.License == "" {
		errs = append(errs, errors.New("must specify a license type for every match"))
	} else {
		if !hasHeaderLicense(m.License) {
			errs = append(errs, fmt.Errorf("invalid license: %q", m.License))
		}
	}

	return errors.Join(errs...)
}

func (m *staticFile) getDelimiter() delimiter {
	if m.Type != "" {
		return builtinDelimiters[m.Type]
	}
	return m.Delimiter
}

type match struct {
	Name      string    `yaml:"name"`
	Match     string    `yaml:"match"`
	Directory string    `yaml:"directory"`
	Extension string    `yaml:"extension"`
	License   string    `yaml:"license"`
	Short     bool      `yaml:"short"`
	Type      string    `yaml:"type"`
	Delimiter delimiter `yaml:"delimiter"`

	matchRegex *regexp.Regexp
}

func (m *match) getDelimiter() delimiter {
	if m.Type != "" {
		return builtinDelimiters[m.Type]
	}
	return m.Delimiter
}

func (m *match) initializeAndValidate(checkLicense bool) error {
	errs := []error{}

	if m.Match != "" {
		matchRegex, err := regexp.Compile(strings.TrimSpace(m.Match))
		if err != nil {
			errs = append(errs, err)
		}
		m.matchRegex = matchRegex
	}

	if checkLicense {
		if m.License == "" {
			errs = append(errs, errors.New("must specify a license type for every match"))
		} else {
			if !m.Short {
				if !hasHeaderLicense(m.License) {
					errs = append(errs, fmt.Errorf("invalid license: %q", m.License))
				}
			} else {
				if !hasShortHeaderLicense(m.License) {
					errs = append(errs, fmt.Errorf("invalid license: %q", m.License))
				}
			}
		}

		if m.Type != "" {
			if _, ok := builtinDelimiters[m.Type]; !ok {
				errs = append(errs, fmt.Errorf("invalid builtin delimiter type: %q", m.Type))
			}
		}

		if m.Type == "" && m.Delimiter == emptyDelimiter {
			errs = append(errs, errors.New("must either specify a delimiter or builtin delimiter type"))
		}

		if m.Type != "" && m.Delimiter != emptyDelimiter {
			errs = append(errs, errors.New("must only specify one of delimiter or builtin delimiter type"))
		}
	}

	if m.hasMultipleMatchers() {
		errs = append(errs, errors.New("must only specify one of name, directory, or match"))
	}

	if !m.hasBaseMatcher() && m.Extension == "" {
		errs = append(errs, errors.New("must specify some match rule"))
	}

	return errors.Join(errs...)
}

func (m *match) hasBaseMatcher() bool {
	return m.Name != "" || m.Match != "" || m.Directory != ""
}

func (m *match) hasMultipleMatchers() bool {
	i := 0
	if m.Name != "" {
		i++
	}
	if m.Match != "" {
		i++
	}
	if m.Directory != "" {
		i++
	}
	return i > 1
}

func (m *match) doExtensionMatch(path string) bool {
	if m.Extension == "" {
		return true
	}

	return filepath.Ext(path) == m.Extension
}

func (m *match) doMatch(path string) bool {
	// first compare name
	if m.Name != "" {
		if filepath.Base(path) == m.Name {
			return m.doExtensionMatch(path)
		}
	}
	// next compare any regex matches
	if m.Match != "" {
		if m.matchRegex.MatchString(path) {
			return m.doExtensionMatch(path)
		}
	}
	// next compare directories
	if m.Directory != "" {
		if strings.HasPrefix(path, m.Directory) {
			return m.doExtensionMatch(path)
		}
	}

	if !m.hasBaseMatcher() {
		// we only want to do a fallback extension match
		// if we have no other filter at this point
		return m.doExtensionMatch(path)
	}

	return false
}

type config struct {
	Path             string        `yaml:"path"`
	TopLevelLicense  string        `yaml:"top_level_license"`
	LicenseDirectory string        `yaml:"license_directory"`
	Licenses         []string      `yaml:"licenses"`
	Matches          []*match      `yaml:"matches"`
	Ignore           []*match      `yaml:"ignore"`
	Files            []*staticFile `yaml:"files"`
}

func loadConfig(path string) (*config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	c := &config{}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, err
	}

	if err := c.initializeAndValidate(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *config) getPath() string {
	if c.Path == "" {
		return "."
	}
	return c.Path
}

func (c *config) getLicenseDirectory() string {
	if c.LicenseDirectory == "" {
		return "licenses"
	}
	return c.LicenseDirectory
}

func (c *config) initializeAndValidate() error {
	errs := []error{}

	if c.TopLevelLicense == "" {
		if len(c.Licenses) == 0 {
			errs = append(errs, errors.New("must specify at least one top-level license for this repo"))
		}
		for _, license := range c.Licenses {
			if !hasLicense(license) {
				errs = append(errs, fmt.Errorf("invalid license: %q", license))
			}
		}
	} else {
		if !hasLicense(c.TopLevelLicense) {
			errs = append(errs, fmt.Errorf("invalid license: %q", c.TopLevelLicense))
		}
	}

	for _, match := range c.Matches {
		if err := match.initializeAndValidate(true); err != nil {
			errs = append(errs, err)
		}
	}

	for _, match := range c.Ignore {
		if err := match.initializeAndValidate(false); err != nil {
			errs = append(errs, err)
		}
	}

	for _, file := range c.Files {
		if err := file.initializeAndValidate(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (c *config) matchFile(path string) (bool, bool, delimiter, string) {
	for _, match := range c.Ignore {
		if match.doMatch(path) {
			return false, false, emptyDelimiter, ""
		}
	}

	for _, match := range c.Matches {
		if match.doMatch(path) {
			return true, match.Short, match.getDelimiter(), match.License
		}
	}

	return false, false, emptyDelimiter, ""
}

func (c *config) walk(ch chan<- *matchedFile) error {
	defer close(ch)
	return filepath.Walk(c.getPath(), func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			return nil
		}

		matched, short, delimiter, license := c.matchFile(path)
		if matched {
			ch <- &matchedFile{path, fi.Mode(), delimiter, license, short}
		}
		return nil
	})
}

func (c *config) writeTopLevelLicense() error {
	if c.TopLevelLicense == "" {
		return nil
	}

	var buf bytes.Buffer
	template := getLicenseTemplate(c.TopLevelLicense)
	if err := template.ExecuteTemplate(&buf, fullName(c.TopLevelLicense), licenseTemplateData); err != nil {
		return err
	}

	if err := writer.Write("LICENSE", buf.Bytes(), 0644); err != nil {
		return err
	}

	return nil
}

func (c *config) writeStaticFiles() error {
	for _, f := range c.Files {
		var buf bytes.Buffer
		template := getHeaderLicenseTemplate(f.License)
		if err := template.ExecuteTemplate(&buf, headerName(f.License), licenseTemplateData); err != nil {
			return err
		}

		err := writeLicenseHeader(
			getHeaderLicenseTemplate(f.License),
			headerName(f.License),
			f.getDelimiter(), false,
			f.Name, 0644, nil,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *config) writeLicenses() error {
	if len(c.Licenses) == 0 {
		return nil
	}

	directory := c.getLicenseDirectory()

	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}

	for _, license := range c.Licenses {
		var buf bytes.Buffer

		filename := path.Join(directory, strings.ToUpper(license)+".md")
		template := getLicenseTemplate(license)
		if err := template.ExecuteTemplate(&buf, fullName(license), licenseTemplateData); err != nil {
			return err
		}

		if err := writer.Write(filename, buf.Bytes(), 0644); err != nil {
			return err
		}
	}

	return nil
}

func getShortHeaderLicenseTemplate(license string) *template.Template {
	license = strings.ToLower(license)
	return shortLicenseHeaderTemplates[license]
}

func getHeaderLicenseTemplate(license string) *template.Template {
	license = strings.ToLower(license)
	return licenseHeaderTemplates[license]
}

func getLicenseTemplate(license string) *template.Template {
	license = strings.ToLower(license)
	return licenseTemplates[license]
}

func hasShortHeaderLicense(license string) bool {
	license = strings.ToLower(license)
	_, ok := shortLicenseHeaderTemplates[license]

	return ok
}

func hasHeaderLicense(license string) bool {
	license = strings.ToLower(license)
	_, ok := licenseHeaderTemplates[license]

	return ok
}

func hasLicense(license string) bool {
	license = strings.ToLower(license)
	_, ok := licenseTemplates[license]

	return ok
}
