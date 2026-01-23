// Copyright 2026 Redpanda Data, Inc.
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
	stderrors "errors"
	"net/http"
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

const (
	PrincipalTypeUser = "User"
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
		return nil, errors.Wrap(err, "failed to verify admin client connectivity")
	}

	return &Client{
		adminClient: adminClient,
	}, nil
}

// Close closes the underlying client connections.
func (c *Client) Close() {
	c.adminClient.Close()
}

// validateRole checks that the role is valid for operations.
// Although Kubernetes enforces non-empty names for resources,
// this defensive check protects against nil pointers and manually
// constructed role objects in tests or internal code.
func validateRole(role *redpandav1alpha2.RedpandaRole) error {
	if role == nil || role.Name == "" {
		return errors.New("role is nil or has empty name")
	}
	return nil
}

// membersToStringSlice converts RoleMember slice to principal string slice.
// Each member is formatted as "PrincipalType:Name".
func membersToStringSlice(members []rpadmin.RoleMember) []string {
	result := make([]string, len(members))
	for i, member := range members {
		result[i] = member.PrincipalType + ":" + member.Name
	}
	return result
}

// Has checks if a role exists in the Redpanda cluster.
func (c *Client) Has(ctx context.Context, role *redpandav1alpha2.RedpandaRole) (bool, error) {
	if err := validateRole(role); err != nil {
		return false, err
	}

	effectiveRoleName := role.GetEffectiveRoleName()
	_, err := c.adminClient.Role(ctx, effectiveRoleName)
	if err != nil {
		// Check if error indicates role doesn't exist using proper error unwrapping
		if isNotFoundError(err) {
			return false, nil
		}
		// Return the error if it's not a "not found" error
		return false, errors.Wrapf(err, "checking if role %s exists", effectiveRoleName)
	}
	return true, nil
}

// Create creates a role in the Redpanda cluster.
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := validateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	// Check if role already exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return errors.Wrapf(err, "checking if role %s already exists", effectiveRoleName)
	}
	if exists {
		return errors.Newf("role %s already exists", effectiveRoleName)
	}

	// Create the role
	_, err = c.adminClient.CreateRole(ctx, effectiveRoleName)
	if err != nil {
		return errors.Wrapf(err, "creating role %s", effectiveRoleName)
	}

	// Assign principals to the role if specified
	if len(role.Spec.Principals) > 0 {
		err = c.updateRoleMembers(ctx, effectiveRoleName, role.Spec.Principals, nil)
		if err != nil {
			// Try to clean up the role if principal assignment fails
			_ = c.adminClient.DeleteRole(ctx, effectiveRoleName, true)
			return errors.Wrapf(err, "assigning principals to role %s", effectiveRoleName)
		}
	}

	return nil
}

// Delete removes a role from the Redpanda cluster.
func (c *Client) Delete(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := validateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	// Delete role and its associated ACLs
	err := c.adminClient.DeleteRole(ctx, effectiveRoleName, true)
	if err != nil {
		// Check if role already doesn't exist (404) - this is not an error for deletion
		if isNotFoundError(err) {
			// Role already doesn't exist, consider deletion successful
			return nil
		}
		return errors.Wrapf(err, "deleting role %s", effectiveRoleName)
	}
	return nil
}

// Update updates an existing role in the Redpanda cluster.
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := validateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	// Check if role exists
	exists, err := c.Has(ctx, role)
	if err != nil {
		return errors.Wrapf(err, "checking if role %s exists", effectiveRoleName)
	}
	if !exists {
		return errors.Newf("role %s does not exist", effectiveRoleName)
	}

	// Get current role members
	currentMembersResp, err := c.adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		return errors.Wrapf(err, "getting current role members for %s", effectiveRoleName)
	}

	// Convert current members to string slice for comparison
	currentPrincipalNames := membersToStringSlice(currentMembersResp.Members)

	// Calculate members to add and remove
	toAdd, toRemove := calculateMembershipChanges(currentPrincipalNames, role.Spec.Principals)

	// Update membership if there are changes
	if len(toAdd) > 0 || len(toRemove) > 0 {
		err = c.updateRoleMembers(ctx, effectiveRoleName, toAdd, toRemove)
		if err != nil {
			return errors.Wrapf(err, "updating role membership for %s", effectiveRoleName)
		}
	}

	return nil
}

// ClearPrincipals removes all principals from a role, used when transitioning
// from managed to unmanaged principals.
func (c *Client) ClearPrincipals(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := validateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	// Get current role members
	currentMembersResp, err := c.adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		return errors.Wrapf(err, "getting current role members for %s", effectiveRoleName)
	}

	// If there are no members, nothing to clear
	if len(currentMembersResp.Members) == 0 {
		return nil
	}

	// Convert all current members to string slice for removal
	currentPrincipalNames := membersToStringSlice(currentMembersResp.Members)

	// Remove all current members
	err = c.updateRoleMembers(ctx, effectiveRoleName, nil, currentPrincipalNames)
	if err != nil {
		return errors.Wrapf(err, "clearing principals for role %s", effectiveRoleName)
	}

	return nil
}

// updateRoleMembers updates role membership by adding and removing principals
func (c *Client) updateRoleMembers(ctx context.Context, roleName string, toAdd, toRemove []string) error {
	if roleName == "" {
		return errors.New("role name cannot be empty")
	}

	// Convert to RoleMember structs using new parsing logic
	addMembers := make([]rpadmin.RoleMember, len(toAdd))
	removeMembers := make([]rpadmin.RoleMember, len(toRemove))

	for i, principal := range toAdd {
		if principal == "" {
			return errors.Newf("principal at index %d is empty", i)
		}
		// Parse principal to extract type and name
		parsed := parsePrincipal(principal)
		addMembers[i] = rpadmin.RoleMember{
			Name:          parsed.Name,
			PrincipalType: parsed.Type,
		}
	}

	for i, principal := range toRemove {
		if principal == "" {
			return errors.Newf("principal at index %d is empty", i)
		}
		// Parse principal to extract type and name
		parsed := parsePrincipal(principal)
		removeMembers[i] = rpadmin.RoleMember{
			Name:          parsed.Name,
			PrincipalType: parsed.Type,
		}
	}

	// Use bulk update for efficiency
	if len(addMembers) > 0 || len(removeMembers) > 0 {
		_, err := c.adminClient.UpdateRoleMembership(ctx, roleName, addMembers, removeMembers, false)
		if err != nil {
			return errors.Wrapf(err, "updating role membership for role %s", roleName)
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
	if stderrors.As(err, &httpErr) {
		return httpErr.Response.StatusCode == http.StatusNotFound
	}
	return false
}

// ParsedPrincipal represents a parsed principal with type and name
type ParsedPrincipal struct {
	Type string
	Name string
}

// parsePrincipal extracts the type and name from a principal string.
// Handles "Type:Name" format and defaults to User type if no prefix.
// Returns ParsedPrincipal with both type and name for better extensibility.
func parsePrincipal(p string) ParsedPrincipal {
	// Handle "Type:Name" format
	if idx := strings.Index(p, ":"); idx > 0 {
		principalType := p[:idx]
		name := p[idx+1:]
		return ParsedPrincipal{
			Type: principalType,
			Name: name,
		}
	}

	// Default to User type if no prefix
	return ParsedPrincipal{
		Type: PrincipalTypeUser,
		Name: p,
	}
}
