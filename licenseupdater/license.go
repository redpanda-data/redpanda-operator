// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"unicode"
)

func writeLicenseHeader(licenseTemplate *template.Template, templateName string, delimiter delimiter, emptyLine bool, path string, mode os.FileMode, data []byte) error {
	var buf bytes.Buffer
	if err := licenseTemplate.ExecuteTemplate(&buf, templateName, licenseTemplateData); err != nil {
		return err
	}

	var out bytes.Buffer
	if delimiter.Top != "" {
		if _, err := fmt.Fprintln(&out, delimiter.Top); err != nil {
			return err
		}
	}
	s := bufio.NewScanner(&buf)
	for s.Scan() {
		mid := delimiter.Middle
		if mid != "" {
			mid += " "
		}

		if _, err := fmt.Fprintln(&out, strings.TrimRightFunc(mid+s.Text(), unicode.IsSpace)); err != nil {
			return err
		}
	}
	if delimiter.Bottom != "" {
		if _, err := fmt.Fprintln(&out, delimiter.Bottom); err != nil {
			return err
		}
	}

	if emptyLine {
		// ensure a newline
		if _, err := fmt.Fprintln(&out, ""); err != nil {
			return err
		}
	}

	s = bufio.NewScanner(bytes.NewReader(data))
	inLicense := false
	skipEmptyFirstLineAfter := false
	lines := 0
	for s.Scan() {
		lines++

		text := s.Text()

		if lines == 1 {
			if delimiter.Top != "" {
				if s.Text() == delimiter.Top {
					inLicense = true
					skipEmptyFirstLineAfter = true
					continue
				}
			} else if strings.HasPrefix(strings.ReplaceAll(text, " ", ""), delimiter.Middle+"Copyright") {
				inLicense = true
				skipEmptyFirstLineAfter = true
				continue
			}
		} else {
			if inLicense {
				if delimiter.Bottom != "" {
					if s.Text() == delimiter.Bottom {
						inLicense = false
						continue
					}
				} else if !strings.HasPrefix(text, delimiter.Middle) {
					inLicense = false
				}
			}
		}

		if inLicense {
			continue
		}

		if skipEmptyFirstLineAfter {
			skipEmptyFirstLineAfter = false
			if text == "" {
				continue
			}
		}

		if _, err := fmt.Fprintln(&out, text); err != nil {
			return err
		}
	}

	return writer.Write(path, out.Bytes(), mode)
}
