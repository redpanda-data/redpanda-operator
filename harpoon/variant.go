// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package framework

import (
	"strings"

	messages "github.com/cucumber/messages/go/v21"
)

func featureVariant(tags []*messages.Tag) string {
	for _, tag := range tags {
		name := strings.TrimPrefix(tag.Name, "@")
		if strings.HasPrefix(name, "variant:") {
			return strings.TrimPrefix(name, "variant:")
		}
	}
	return ""
}
