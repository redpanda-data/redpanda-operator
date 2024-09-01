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
	"text/template"
)

type matchedFile struct {
	path      string
	mode      os.FileMode
	delimiter delimiter
	license   string
	short     bool
}

func (f *matchedFile) process() error {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return err
	}

	var licenseTemplate *template.Template
	var templateName string
	if f.short {
		licenseTemplate = getShortHeaderLicenseTemplate(f.license)
		templateName = shortHeaderName(f.license)
	} else {
		licenseTemplate = getHeaderLicenseTemplate(f.license)
		templateName = headerName(f.license)
	}

	return writeLicenseHeader(licenseTemplate, templateName, f.delimiter, true, f.path, f.mode, data)
}
