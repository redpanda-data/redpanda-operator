// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePrincipal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ParsedPrincipal
	}{
		{
			name:     "user with explicit type",
			input:    "User:alice",
			expected: ParsedPrincipal{Type: PrincipalTypeUser, Name: "alice"},
		},
		{
			name:     "user without type prefix defaults to User",
			input:    "alice",
			expected: ParsedPrincipal{Type: PrincipalTypeUser, Name: "alice"},
		},
		{
			name:     "group with explicit type",
			input:    "Group:engineering",
			expected: ParsedPrincipal{Type: PrincipalTypeGroup, Name: "engineering"},
		},
		{
			name:     "group with complex name",
			input:    "Group:team-platform-engineers",
			expected: ParsedPrincipal{Type: PrincipalTypeGroup, Name: "team-platform-engineers"},
		},
		{
			name:     "group with colon in name",
			input:    "Group:org:team",
			expected: ParsedPrincipal{Type: PrincipalTypeGroup, Name: "org:team"},
		},
		{
			name:     "user with colon in name",
			input:    "User:domain:user",
			expected: ParsedPrincipal{Type: PrincipalTypeUser, Name: "domain:user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePrincipal(tt.input)
			require.Equal(t, tt.expected.Type, result.Type, "principal type should match")
			require.Equal(t, tt.expected.Name, result.Name, "principal name should match")
		})
	}
}

func TestCalculateMembershipChanges(t *testing.T) {
	tests := []struct {
		name           string
		current        []string
		desired        []string
		expectedAdd    []string
		expectedRemove []string
	}{
		{
			name:           "no changes needed",
			current:        []string{"User:alice", "User:bob"},
			desired:        []string{"User:alice", "User:bob"},
			expectedAdd:    []string{},
			expectedRemove: []string{},
		},
		{
			name:           "add new members",
			current:        []string{"User:alice"},
			desired:        []string{"User:alice", "User:bob", "User:charlie"},
			expectedAdd:    []string{"User:bob", "User:charlie"},
			expectedRemove: []string{},
		},
		{
			name:           "remove members",
			current:        []string{"User:alice", "User:bob", "User:charlie"},
			desired:        []string{"User:alice"},
			expectedAdd:    []string{},
			expectedRemove: []string{"User:bob", "User:charlie"},
		},
		{
			name:           "replace all members",
			current:        []string{"User:alice", "User:bob"},
			desired:        []string{"User:charlie", "User:dave"},
			expectedAdd:    []string{"User:charlie", "User:dave"},
			expectedRemove: []string{"User:alice", "User:bob"},
		},
		{
			name:           "empty to some",
			current:        []string{},
			desired:        []string{"User:alice"},
			expectedAdd:    []string{"User:alice"},
			expectedRemove: []string{},
		},
		{
			name:           "some to empty",
			current:        []string{"User:alice"},
			desired:        []string{},
			expectedAdd:    []string{},
			expectedRemove: []string{"User:alice"},
		},
		{
			name:           "add group members",
			current:        []string{"User:alice"},
			desired:        []string{"User:alice", "Group:engineering"},
			expectedAdd:    []string{"Group:engineering"},
			expectedRemove: []string{},
		},
		{
			name:           "remove group members",
			current:        []string{"User:alice", "Group:engineering", "Group:platform"},
			desired:        []string{"User:alice"},
			expectedAdd:    []string{},
			expectedRemove: []string{"Group:engineering", "Group:platform"},
		},
		{
			name:           "mixed user and group changes",
			current:        []string{"User:alice", "Group:engineering"},
			desired:        []string{"User:bob", "Group:platform"},
			expectedAdd:    []string{"User:bob", "Group:platform"},
			expectedRemove: []string{"User:alice", "Group:engineering"},
		},
		{
			name:           "replace users with groups",
			current:        []string{"User:alice", "User:bob"},
			desired:        []string{"Group:engineering", "Group:platform"},
			expectedAdd:    []string{"Group:engineering", "Group:platform"},
			expectedRemove: []string{"User:alice", "User:bob"},
		},
		{
			name:           "group only no changes",
			current:        []string{"Group:engineering", "Group:platform"},
			desired:        []string{"Group:engineering", "Group:platform"},
			expectedAdd:    []string{},
			expectedRemove: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toAdd, toRemove := CalculateMembershipChanges(tt.current, tt.desired)

			require.ElementsMatch(t, tt.expectedAdd, toAdd, "toAdd should match expected")
			require.ElementsMatch(t, tt.expectedRemove, toRemove, "toRemove should match expected")
		})
	}
}
