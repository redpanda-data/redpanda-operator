// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testing

import (
	"context"
	"sort"
	"strings"
)

// CleanupFunc is a function run during a cleanup execution.
type CleanupFunc func(context.Context)

type ParsedTagHandler struct {
	Priority  int
	Arguments []string
	Handler   func(context.Context, *TestingT, []string) context.Context
}

type PriorityTagHandler struct {
	Priority int
	Handler  func(context.Context, *TestingT, []string) context.Context
}

type TagRegistry struct {
	tags map[string]PriorityTagHandler
}

func NewTagRegistry() *TagRegistry {
	return &TagRegistry{
		tags: make(map[string]PriorityTagHandler),
	}
}

func (r *TagRegistry) Register(tag string, priority int, handler func(context.Context, *TestingT, []string) context.Context) {
	r.tags[tag] = PriorityTagHandler{
		Handler:  handler,
		Priority: priority,
	}
}

func (r *TagRegistry) Handlers(tags []string) []ParsedTagHandler {
	handlers := []ParsedTagHandler{}

	for _, tag := range tags {
		tokens := strings.Split(tag, ":")
		tag = tokens[0]

		args := []string{}
		if len(tokens) > 1 {
			args = tokens[1:]
		}

		if handler, ok := r.tags[tag]; ok {
			handlers = append(handlers, ParsedTagHandler{
				Arguments: args,
				Priority:  handler.Priority,
				Handler:   handler.Handler,
			})
		}
	}

	sort.SliceStable(handlers, func(i, j int) bool {
		a, b := handlers[i], handlers[j]
		return a.Priority < b.Priority
	})

	return handlers
}
