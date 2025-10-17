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
// roleBindings should contain all RoleBindings that reference this role.
// Returns the list of principals that were successfully synced.
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.RedpandaRole, roleBindings []*redpandav1alpha2.RedpandaRoleBinding) ([]string, error) {
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
	principals := aggregatePrincipals(role, roleBindings)

	// Assign principals to the role if specified
	if len(principals) > 0 {
		err = c.updateRoleMembers(ctx, role.Name, principals, nil)
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
// roleBindings should contain all RoleBindings that reference this role.
// If roleBindings is nil or empty and role has no inline principals, membership is not managed.
// Returns the list of principals that were successfully synced, or empty slice if in manual management mode.
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.RedpandaRole, roleBindings []*redpandav1alpha2.RedpandaRoleBinding) ([]string, error) {
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
	currentPrincipalNames := make([]string, len(currentMembersResp.Members))
	for i, member := range currentMembersResp.Members {
		// Reconstruct principal format: "Type:Name"
		currentPrincipalNames[i] = member.PrincipalType + ":" + member.Name
	}

	// Get all desired principals from Role and RoleBindings
	desiredPrincipals := aggregatePrincipals(role, roleBindings)

	// Get previously managed principals from status (what we synced last time)
	previouslyManaged := role.Status.Principals

	// Skip membership management if no principals are defined AND we have nothing to clean up
	// This allows manual management of role membership outside the operator
	if len(desiredPrincipals) == 0 && len(previouslyManaged) == 0 {
		// No principals defined in Role or RoleBindings and nothing previously managed - don't touch membership
		// Return empty slice to indicate manual management mode
		return []string{}, nil
	}

	// Calculate members to add and remove
	// Only remove principals WE previously managed (protects manual additions)
	toAdd, toRemove := calculateMembershipChanges(currentPrincipalNames, desiredPrincipals, previouslyManaged)

	// Update membership if there are changes
	if len(toAdd) > 0 || len(toRemove) > 0 {
		err = c.updateRoleMembers(ctx, role.Name, toAdd, toRemove)
		if err != nil {
			return nil, fmt.Errorf("updating role membership for %s: %w", role.Name, err)
		}
	}

	// Return the desired principals we successfully synced
	return desiredPrincipals, nil
}

// updateRoleMembers updates role membership by adding and removing principals
func (c *Client) updateRoleMembers(ctx context.Context, roleName string, toAdd, toRemove []string) error {
	// Convert to RoleMember structs - parse principal type and name
	addMembers := make([]rpadmin.RoleMember, len(toAdd))
	removeMembers := make([]rpadmin.RoleMember, len(toRemove))

	for i, principal := range toAdd {
		// Parse principal to extract type and name
		principalType, name := parsePrincipal(principal)
		addMembers[i] = rpadmin.RoleMember{
			Name:          name,
			PrincipalType: principalType,
		}
	}

	for i, principal := range toRemove {
		// Parse principal to extract type and name
		principalType, name := parsePrincipal(principal)
		removeMembers[i] = rpadmin.RoleMember{
			Name:          name,
			PrincipalType: principalType,
		}
	}

	// Use bulk update for efficiency
	if len(addMembers) > 0 || len(removeMembers) > 0 {
		_, err := c.adminClient.UpdateRoleMembership(ctx, roleName, addMembers, removeMembers, false)
		if err != nil {
			return fmt.Errorf("updating role membership for role %s: %w", roleName, err)
		}
	}

	return nil
}

// calculateMembershipChanges determines which principals to add and remove.
// It only removes principals that were previously managed by the operator (in previouslyManaged).
// This protects manually added principals from being removed.
func calculateMembershipChanges(current, desired, previouslyManaged []string) (toAdd, toRemove []string) {
	// Find principals to add (in desired but not in current)
	for _, principal := range desired {
		if !slices.Contains(current, principal) {
			toAdd = append(toAdd, principal)
		}
	}

	// Only remove principals that WE previously managed AND are no longer desired
	// This protects manually added principals (not in previouslyManaged)
	for _, principal := range previouslyManaged {
		if !slices.Contains(desired, principal) {
			// We managed it before, but it's no longer desired - remove it
			toRemove = append(toRemove, principal)
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

const (
	defaultPrincipalType = "User"
)

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

// aggregatePrincipals merges principals from a Role and its RoleBindings.
// Duplicates are automatically removed by normalizing all principals to "Type:Name" format.
func aggregatePrincipals(role *redpandav1alpha2.RedpandaRole, roleBindings []*redpandav1alpha2.RedpandaRoleBinding) []string {
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

	// Add principals from all RoleBindings
	for _, rb := range roleBindings {
		for _, p := range rb.Spec.Principals {
			if p != "" {
				normalized := NormalizePrincipal(p)
				if _, exists := seen[normalized]; !exists {
					seen[normalized] = struct{}{}
					principals = append(principals, normalized)
				}
			}
		}
	}

	return principals
}
