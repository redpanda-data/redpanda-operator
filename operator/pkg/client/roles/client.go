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
func (c *Client) Has(ctx context.Context, role *redpandav1alpha2.Role) (bool, error) {
	if role == nil || role.Name == "" {
		return false, fmt.Errorf("role is nil or has empty name")
	}

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
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.Role) error {
	if role == nil || role.Name == "" {
		return fmt.Errorf("role is nil or has empty name")
	}

	// Check if role already exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return fmt.Errorf("checking if role %s already exists: %w", role.Name, err)
	}
	if exists {
		return fmt.Errorf("role %s already exists", role.Name)
	}

	// Create the role
	_, err = c.adminClient.CreateRole(ctx, role.Name)
	if err != nil {
		return fmt.Errorf("creating role %s: %w", role.Name, err)
	}

	// Assign principals to the role if specified
	if len(role.Spec.Principals) > 0 {
		err = c.updateRoleMembers(ctx, role.Name, role.Spec.Principals, nil)
		if err != nil {
			// Try to clean up the role if principal assignment fails
			_ = c.adminClient.DeleteRole(ctx, role.Name, true)
			return fmt.Errorf("assigning principals to role %s: %w", role.Name, err)
		}
	}

	return nil
}

// Delete removes a role from the Redpanda cluster.
func (c *Client) Delete(ctx context.Context, role *redpandav1alpha2.Role) error {
	if role == nil || role.Name == "" {
		return fmt.Errorf("role is nil or has empty name")
	}

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
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.Role) error {
	if role == nil || role.Name == "" {
		return fmt.Errorf("role is nil or has empty name")
	}

	// Check if role exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return fmt.Errorf("checking if role %s exists: %w", role.Name, err)
	}
	if !exists {
		return fmt.Errorf("role %s does not exist", role.Name)
	}

	// Get current role members
	currentMembersResp, err := c.adminClient.RoleMembers(ctx, role.Name)
	if err != nil {
		return fmt.Errorf("getting current role members for %s: %w", role.Name, err)
	}

	// Convert current members to string slice for comparison
	currentPrincipalNames := make([]string, len(currentMembersResp.Members))
	for i, member := range currentMembersResp.Members {
		// Reconstruct principal format: "Type:Name"
		currentPrincipalNames[i] = member.PrincipalType + ":" + member.Name
	}

	// Calculate members to add and remove
	toAdd, toRemove := calculateMembershipChanges(currentPrincipalNames, role.Spec.Principals)

	// Update membership if there are changes
	if len(toAdd) > 0 || len(toRemove) > 0 {
		err = c.updateRoleMembers(ctx, role.Name, toAdd, toRemove)
		if err != nil {
			return fmt.Errorf("updating role membership for %s: %w", role.Name, err)
		}
	}

	return nil
}

// updateRoleMembers updates role membership by adding and removing principals
func (c *Client) updateRoleMembers(ctx context.Context, roleName string, toAdd, toRemove []string) error {
	if roleName == "" {
		return fmt.Errorf("role name cannot be empty")
	}

	// Convert to RoleMember structs - extract username from "User:username" format
	addMembers := make([]rpadmin.RoleMember, len(toAdd))
	removeMembers := make([]rpadmin.RoleMember, len(toRemove))

	for i, principal := range toAdd {
		if principal == "" {
			return fmt.Errorf("principal at index %d is empty", i)
		}
		// Parse principal to extract username
		name := parsePrincipal(principal)
		addMembers[i] = rpadmin.RoleMember{
			Name:          name,
			PrincipalType: principalTypeUser,
		}
	}

	for i, principal := range toRemove {
		if principal == "" {
			return fmt.Errorf("principal at index %d is empty", i)
		}
		// Parse principal to extract username
		name := parsePrincipal(principal)
		removeMembers[i] = rpadmin.RoleMember{
			Name:          name,
			PrincipalType: principalTypeUser,
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

// calculateMembershipChanges determines which principals to add and remove
func calculateMembershipChanges(current, desired []string) (toAdd, toRemove []string) {
	// Find principals to add (in desired but not in current)
	for _, principal := range desired {
		if !slices.Contains(current, principal) {
			toAdd = append(toAdd, principal)
		}
	}

	// Find principals to remove (in current but not in desired)
	for _, principal := range current {
		if !slices.Contains(desired, principal) {
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
	userPrefix        = "User:"
	principalTypeUser = "User"
)

// parsePrincipal extracts the username from a principal string.
// Handles "User:username" format and defaults to treating the whole string as username if no prefix.
// Currently only supports User principals.
func parsePrincipal(p string) string {
	if name, found := strings.CutPrefix(p, userPrefix); found {
		return name
	}
	// Default to treating the whole string as username
	return p
}
