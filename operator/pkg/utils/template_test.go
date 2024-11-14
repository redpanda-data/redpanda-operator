// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package utils_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
)

func TestTemplateGen(t *testing.T) {
	data := utils.NewEndpointTemplateData(2, "1.1.1.1", 100)
	tests := []struct {
		tmpl                 string
		endpointContainsPort bool
		expected             string
		error                bool
	}{
		{
			tmpl:     "",
			expected: "2", // Index
		},
		{
			tmpl:     "{{.Index}}",
			expected: "2",
		},
		{
			tmpl:     "{{.Index}}-{{.HostIP | sha256sum | substr 0 8}}",
			expected: "2-f1412386",
		},
		{
			tmpl:  "abc-{{.Index}}-{{.HostIP}}-xyz",
			error: true, // HostIP contains dots
		},
		{
			tmpl:  `{{ "$USER" | expandenv }}`,
			error: true, // expandenv is not hermetic
		},
		{
			tmpl:  "aa{{.XX}}",
			error: true, // undefined variable
		},
		{
			tmpl:  "-{{.Index}}",
			error: true, // invalid start character
		},
		{
			tmpl:  "{{.Index}}-",
			error: true, // invalid end character
		},
		{
			tmpl:                 "'address': '{{.Index}}-{{.HostIP | sha256sum | substr 0 8}}.redpanda.com', 'port': {{39002 | add .Index}}",
			expected:             "'address': '2-f1412386.redpanda.com', 'port': 39004",
			endpointContainsPort: true,
		},
		{
			tmpl:                 "'address': '{{.Index}}-{{.HostIP | sha256sum | substr 0 8}}.redpanda.com', 'port': {{30092 | add (.Index | sub .Index )}}",
			expected:             "'address': '2-f1412386.redpanda.com', 'port': 30092",
			endpointContainsPort: true,
		},
		{
			tmpl:                 "'address': '{{.Index}}-{{.HostIP | sha256sum | substr 0 8}}.redpanda.com', 'port': {{30092 | add (.Index | sub .Index | add .HostIndexOffset )}}",
			expected:             "'address': '2-f1412386.redpanda.com', 'port': 30192",
			endpointContainsPort: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.tmpl, func(t *testing.T) {
			res, err := utils.Compute(tc.tmpl, data, !tc.endpointContainsPort)
			assert.Equal(t, tc.expected, res)
			if tc.error {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
