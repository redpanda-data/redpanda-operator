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

	adminv2 "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	rolesinternal "github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles/internal"
	rolesv2 "github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles/v2"
)

// Re-export constants and types from the internal package for external use.
const (
	PrincipalTypeUser  = rolesinternal.PrincipalTypeUser
	PrincipalTypeGroup = rolesinternal.PrincipalTypeGroup
)

// ParsedPrincipal represents a parsed principal with type and name.
type ParsedPrincipal = rolesinternal.ParsedPrincipal

// Option is a functional option for configuring the Client.
type Option func(*clientOptions)

type clientOptions struct {
	disableV2 bool
}

// WithV2Disabled disables the v2 SecurityService API probe and forces
// the client to use the v1 REST admin API. This is useful in tests
// to verify the v1 code path.
func WithV2Disabled() Option {
	return func(o *clientOptions) {
		o.disableV2 = true
	}
}

// Client is a high-level client for managing roles in a Redpanda cluster.
// It transparently delegates to the v2 SecurityService API when available,
// falling back to the v1 REST admin API for older clusters.
type Client struct {
	adminClient *rpadmin.AdminAPI
	v2Client    *rolesv2.Client
}

// NewClient returns a high-level client that is able to manage roles in a Redpanda cluster.
// By default, it probes whether the cluster supports the v2 SecurityService API and
// delegates to it if available. Use WithV2Disabled() to force the v1 code path.
func NewClient(ctx context.Context, adminClient *rpadmin.AdminAPI, opts ...Option) (*Client, error) {
	var options clientOptions
	for _, opt := range opts {
		opt(&options)
	}

	// Verify admin client connectivity
	_, err := adminClient.Brokers(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to verify admin client connectivity")
	}

	c := &Client{
		adminClient: adminClient,
	}

	// Probe v2 API availability
	if !options.disableV2 {
		_, err := adminClient.SecurityService().ListRoles(ctx, connect.NewRequest(
			adminv2.ListRolesRequest_builder{}.Build(),
		))
		if err == nil {
			c.v2Client = rolesv2.NewClient(adminClient)
		}
	}

	return c, nil
}

// Close closes the underlying client connections.
func (c *Client) Close() {
	c.adminClient.Close()
}

// SupportsGroups reports whether the cluster supports Group principals
// in role membership (i.e., the v2 SecurityService API is available).
func (c *Client) SupportsGroups() bool {
	return c.v2Client != nil
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
	if c.v2Client != nil {
		return c.v2Client.Has(ctx, role)
	}

	if err := rolesinternal.ValidateRole(role); err != nil {
		return false, err
	}

	effectiveRoleName := role.GetEffectiveRoleName()
	_, err := c.adminClient.Role(ctx, effectiveRoleName)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "checking if role %s exists", effectiveRoleName)
	}
	return true, nil
}

// Create creates a role in the Redpanda cluster.
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if c.v2Client != nil {
		return c.v2Client.Create(ctx, role)
	}

	if err := rolesinternal.ValidateRole(role); err != nil {
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
	if c.v2Client != nil {
		return c.v2Client.Delete(ctx, role)
	}

	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	err := c.adminClient.DeleteRole(ctx, effectiveRoleName, true)
	if err != nil {
		if isNotFoundError(err) {
			return nil
		}
		return errors.Wrapf(err, "deleting role %s", effectiveRoleName)
	}
	return nil
}

// Update updates an existing role in the Redpanda cluster.
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if c.v2Client != nil {
		return c.v2Client.Update(ctx, role)
	}

	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	exists, err := c.Has(ctx, role)
	if err != nil {
		return errors.Wrapf(err, "checking if role %s exists", effectiveRoleName)
	}
	if !exists {
		return errors.Newf("role %s does not exist", effectiveRoleName)
	}

	currentMembersResp, err := c.adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		return errors.Wrapf(err, "getting current role members for %s", effectiveRoleName)
	}

	currentPrincipalNames := membersToStringSlice(currentMembersResp.Members)
	toAdd, toRemove := rolesinternal.CalculateMembershipChanges(currentPrincipalNames, role.Spec.Principals)

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
	if c.v2Client != nil {
		return c.v2Client.ClearPrincipals(ctx, role)
	}

	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	currentMembersResp, err := c.adminClient.RoleMembers(ctx, effectiveRoleName)
	if err != nil {
		return errors.Wrapf(err, "getting current role members for %s", effectiveRoleName)
	}

	if len(currentMembersResp.Members) == 0 {
		return nil
	}

	currentPrincipalNames := membersToStringSlice(currentMembersResp.Members)

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

	addMembers := make([]rpadmin.RoleMember, len(toAdd))
	removeMembers := make([]rpadmin.RoleMember, len(toRemove))

	for i, principal := range toAdd {
		if principal == "" {
			return errors.Newf("principal at index %d is empty", i)
		}
		parsed := rolesinternal.ParsePrincipal(principal)
		addMembers[i] = rpadmin.RoleMember{
			Name:          parsed.Name,
			PrincipalType: parsed.Type,
		}
	}

	for i, principal := range toRemove {
		if principal == "" {
			return errors.Newf("principal at index %d is empty", i)
		}
		parsed := rolesinternal.ParsePrincipal(principal)
		removeMembers[i] = rpadmin.RoleMember{
			Name:          parsed.Name,
			PrincipalType: parsed.Type,
		}
	}

	if len(addMembers) > 0 || len(removeMembers) > 0 {
		_, err := c.adminClient.UpdateRoleMembership(ctx, roleName, addMembers, removeMembers, false)
		if err != nil {
			return errors.Wrapf(err, "updating role membership for role %s", roleName)
		}
	}

	return nil
}

// isNotFoundError checks if the error is a 404 Not Found HTTP error
func isNotFoundError(err error) bool {
	var httpErr *rpadmin.HTTPResponseError
	if stderrors.As(err, &httpErr) {
		return httpErr.Response.StatusCode == http.StatusNotFound
	}
	return false
}
