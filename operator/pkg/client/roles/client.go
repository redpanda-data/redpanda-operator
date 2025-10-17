// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package roles

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	defaultPrincipalType = "User"
)

// Client is a high-level client for managing roles in a Redpanda cluster.
type Client struct {
	adminClient *rpadmin.AdminAPI
}

// NewClient returns a high-level client that is able to manage roles in a Redpanda cluster.
func NewClient(ctx context.Context, adminClient *rpadmin.AdminAPI) (*Client, error) {
	// Verify admin client connectivity (similar to how users client verifies API versions)
	_, err := adminClient.Brokers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify admin client connectivity: %w", err)
	}

	return &Client{
		adminClient: adminClient,
	}, nil
}

// Close closes the underlying client connections.
func (c *Client) Close() {
	c.adminClient.Close()
}

// Has checks if a role exists in the Redpanda cluster.
func (c *Client) Has(ctx context.Context, role *redpandav1alpha2.RedpandaRole) (bool, error) {
	_, err := c.adminClient.Role(ctx, role.Name)
	if err != nil {
		// Check if error indicates role doesn't exist using proper error unwrapping
		if isNotFoundError(err) {
			return false, nil
		}
		// Return the error if it's not a "not found" error
		return false, fmt.Errorf("checking if role %s exists: %w", role.Name, err)
	}
	return true, nil
}

// Create creates a role in the Redpanda cluster.
// Returns the list of principals that were successfully synced.
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.RedpandaRole) ([]string, error) {
	// Check if role already exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("checking if role %s already exists: %w", role.Name, err)
	}
	if exists {
		return nil, fmt.Errorf("role %s already exists", role.Name)
	}

	// Create the role
	_, err = c.adminClient.CreateRole(ctx, role.Name)
	if err != nil {
		return nil, fmt.Errorf("creating role %s: %w", role.Name, err)
	}

	// Get all principals from Role and RoleBindings
	principals := normalizePrincipals(role)

	// Assign principals to the role if specified
	if len(principals) > 0 {
		_, err = c.Update(ctx, role)
		if err != nil {
			// Try to clean up the role if principal assignment fails
			_ = c.adminClient.DeleteRole(ctx, role.Name, true)
			return nil, fmt.Errorf("assigning principals to role %s: %w", role.Name, err)
		}
	}

	// Return the principals we successfully synced
	return principals, nil
}

// Delete removes a role from the Redpanda cluster.
func (c *Client) Delete(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	// Delete role and its associated ACLs
	err := c.adminClient.DeleteRole(ctx, role.Name, true)
	if err != nil {
		// Check if role already doesn't exist (404) - this is not an error for deletion
		if isNotFoundError(err) {
			// Role already doesn't exist, consider deletion successful
			return nil
		}
		return fmt.Errorf("deleting role %s: %w", role.Name, err)
	}
	return nil
}

// Update updates an existing role in the Redpanda cluster.
// It manages membership using inline spec.principals only; aggregation from
// RoleBindings is not implemented yet. If no inline principals are defined and
// none were previously managed, membership is left untouched (manual mode).
// Returns the list of principals that were successfully synced, or empty slice
// when in manual management mode.
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.RedpandaRole) ([]string, error) {
	// Check if role exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("checking if role %s exists: %w", role.Name, err)
	}
	if !exists {
		return nil, fmt.Errorf("role %s does not exist", role.Name)
	}

	// Get current role members
	currentMembersResp, err := c.adminClient.RoleMembers(ctx, role.Name)
	if err != nil {
		return nil, fmt.Errorf("getting current role members for %s: %w", role.Name, err)
	}

	// Convert current members to string slice for comparison
	currentPrincipals := make([]string, len(currentMembersResp.Members))
	for i, member := range currentMembersResp.Members {
		// Reconstruct principal format: "Type:Name"
		currentPrincipals[i] = member.PrincipalType + ":" + member.Name
	}

	// Get previously managed principals from status (what we synced last time)
	previouslyManaged := role.Status.Principals

	// the new principals we want to set
	desiredPrincipals := normalizePrincipals(role)

	// Skip membership management if no principals are defined AND we have nothing to clean up
	// This allows manual management of role membership outside the operator
	if len(desiredPrincipals) == 0 && len(previouslyManaged) == 0 {
		// No principals defined in Role or RoleBindings and nothing previously managed - don't touch membership
		// Return empty slice to indicate manual management mode
		return []string{}, nil
	}

	// Calculate members to add and remove
	// Only remove principals WE previously managed (protects manual additions)
	toAdd, toRemove := calculateMembershipChanges(currentPrincipals, desiredPrincipals, previouslyManaged)

	// Update membership if there are changes
	if len(toAdd) > 0 || len(toRemove) > 0 {
		_, err := c.adminClient.UpdateRoleMembership(ctx, role.Name, toAdd, toRemove, false)
		if err != nil {
			return nil, fmt.Errorf("updating role membership for %s: %w", role.Name, err)
		}
	}

	// Return the desired principals we successfully synced
	return desiredPrincipals, nil
}

// calculateMembershipChanges determines which principals to add and remove.
// It only removes principals that were previously managed by the operator (in previouslyManaged).
// This protects manually added principals from being removed.
func calculateMembershipChanges(current, desired, previouslyManaged []string) (toAdd, toRemove []rpadmin.RoleMember) {
	// Find principals to add (in desired but not in current)
	for _, principal := range desired {
		if !slices.Contains(current, principal) {
			principalType, name := parsePrincipal(principal)
			toAdd = append(toAdd, rpadmin.RoleMember{
				Name:          name,
				PrincipalType: principalType,
			})
		}
	}

	// Only remove principals that WE previously managed AND are no longer desired
	// This protects manually added principals (not in previouslyManaged)
	for _, principal := range previouslyManaged {
		if !slices.Contains(desired, principal) {
			// We managed it before, but it's no longer desired - remove it
			principalType, name := parsePrincipal(principal)
			toRemove = append(toRemove, rpadmin.RoleMember{
				Name:          name,
				PrincipalType: principalType,
			})
		}
	}

	return toAdd, toRemove
}

// isNotFoundError checks if the error is a 404 Not Found HTTP error
func isNotFoundError(err error) bool {
	var httpErr *rpadmin.HTTPResponseError
	if errors.As(err, &httpErr) {
		return httpErr.Response.StatusCode == http.StatusNotFound
	}
	return false
}

// parsePrincipal extracts the principal type and name from a principal string.
// Handles "Type:name" format (e.g., "User:alice") or plain name (e.g., "alice").
// If no type prefix is provided, defaults to "User" type.
// Note: Redpanda role membership currently only supports User principals.
func parsePrincipal(p string) (principalType, name string) {
	// Check if principal has "Type:Name" format
	if idx := strings.Index(p, ":"); idx > 0 {
		return p[:idx], p[idx+1:]
	}
	// Default to User type if no prefix
	return defaultPrincipalType, p
}

// NormalizePrincipal converts a principal to the canonical "Type:Name" format.
// This ensures that "alice" and "User:alice" are treated as the same principal.
func NormalizePrincipal(p string) string {
	principalType, name := parsePrincipal(p)
	return principalType + ":" + name
}

// normalizePrincipals duplicates normalizing all principals to "Type:Name" format.
func normalizePrincipals(role *redpandav1alpha2.RedpandaRole) []string {
	// Use a map to track unique principals (using normalized format as key)
	seen := make(map[string]struct{})
	var principals []string

	// Add inline principals from the Role itself (backwards compatible)
	for _, p := range role.Spec.Principals {
		if p != "" {
			normalized := NormalizePrincipal(p)
			if _, exists := seen[normalized]; !exists {
				seen[normalized] = struct{}{}
				principals = append(principals, normalized)
			}
		}
	}

	return principals
}
