// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package internal provides shared utilities for the roles client packages.
package internal

import (
	"slices"
	"strings"

	"github.com/cockroachdb/errors"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	PrincipalTypeUser  = "User"
	PrincipalTypeGroup = "Group"
)

// ParsedPrincipal represents a parsed principal with type and name.
type ParsedPrincipal struct {
	Type string
	Name string
}

// ParsePrincipal extracts the type and name from a principal string.
// Handles "Type:Name" format and defaults to User type if no prefix.
func ParsePrincipal(p string) ParsedPrincipal {
	if idx := strings.Index(p, ":"); idx > 0 {
		return ParsedPrincipal{
			Type: p[:idx],
			Name: p[idx+1:],
		}
	}
	return ParsedPrincipal{
		Type: PrincipalTypeUser,
		Name: p,
	}
}

// ValidateRole checks that the role is valid for operations.
func ValidateRole(role *redpandav1alpha2.RedpandaRole) error {
	if role == nil || role.Name == "" {
		return errors.New("role is nil or has empty name")
	}
	return nil
}

// CalculateMembershipChanges determines which principals to add and remove.
func CalculateMembershipChanges(current, desired []string) (toAdd, toRemove []string) {
	for _, principal := range desired {
		if !slices.Contains(current, principal) {
			toAdd = append(toAdd, principal)
		}
	}
	for _, principal := range current {
		if !slices.Contains(desired, principal) {
			toRemove = append(toRemove, principal)
		}
	}
	return toAdd, toRemove
}
