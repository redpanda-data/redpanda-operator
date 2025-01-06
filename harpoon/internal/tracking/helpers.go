// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package tracking

import (
	"strings"

	messages "github.com/cucumber/messages/go/v21"
)

type tagset struct {
	tags map[string]int
}

func newTagset() *tagset {
	return &tagset{
		tags: make(map[string]int),
	}
}

func (t *tagset) clone() *tagset {
	cloned := newTagset()
	for k, v := range t.tags {
		cloned.tags[k] = v
	}
	return cloned
}

func (t *tagset) add(value string) {
	v := t.tags[value]
	v++
	t.tags[value] = v
}

func (t *tagset) flatten() []string {
	flattened := []string{}
	for tag := range t.tags {
		flattened = append(flattened, tag)
	}
	return flattened
}

func (t *tagset) checkAvailablity(value string) bool {
	tag, found := t.tags[value]
	if !found {
		return true
	}
	tag--

	if tag < 0 {
		delete(t.tags, value)
		return true
	}

	t.tags[value] = tag
	return false
}

func cleanTag(tag string) string {
	return strings.TrimPrefix(tag, "@")
}

func tagsForScenario(featureTags *tagset, tags []*messages.PickleTag) *tagset {
	// we need to filter the tags out one time since scenarios
	// inherit tags from their parent features, instead we just
	// remove one tag from the parent, and if there are any left
	// then we
	filteredTags := featureTags.clone()
	scenarioTags := newTagset()

	for _, tag := range tags {
		tagName := cleanTag(tag.Name)

		if filteredTags.checkAvailablity(tagName) {
			scenarioTags.add(tagName)
		}
	}

	return scenarioTags
}

func tagsForFeature(tags []*messages.Tag) *tagset {
	featureTags := newTagset()

	for _, tag := range tags {
		featureTags.add(cleanTag(tag.Name))
	}

	return featureTags
}

func FilterChildren(provider string, children []*messages.FeatureChild) []*messages.FeatureChild {
	filtered := []*messages.FeatureChild{}
	for _, child := range children {
		if child.Scenario == nil {
			continue
		}
		skip := false
		for _, tag := range child.Scenario.Tags {
			if tag.Name == "@skip:"+provider {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, child)
		}
	}
	return filtered
}
