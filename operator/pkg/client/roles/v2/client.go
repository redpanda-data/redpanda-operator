// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package v2 provides a roles client using the v2 SecurityService protobuf API,
// which supports both User and Group principals in role membership.
package v2

import (
	"context"

	adminv2 "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	rolesinternal "github.com/redpanda-data/redpanda-operator/operator/pkg/client/roles/internal"
)

// Client is a high-level client for managing roles via the v2 admin API,
// which supports both User and Group principals in role membership.
type Client struct {
	adminClient *rpadmin.AdminAPI
}

// NewClient returns a v2 SecurityService client. The caller is expected to
// have already verified admin client connectivity.
func NewClient(adminClient *rpadmin.AdminAPI) *Client {
	return &Client{
		adminClient: adminClient,
	}
}

// Has checks if a role exists in the Redpanda cluster.
func (c *Client) Has(ctx context.Context, role *redpandav1alpha2.RedpandaRole) (bool, error) {
	if err := rolesinternal.ValidateRole(role); err != nil {
		return false, err
	}

	effectiveRoleName := role.GetEffectiveRoleName()
	_, err := c.adminClient.SecurityService().GetRole(ctx, connect.NewRequest(
		adminv2.GetRoleRequest_builder{Name: effectiveRoleName}.Build(),
	))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return false, nil
		}
		return false, errors.Wrapf(err, "checking if role %s exists", effectiveRoleName)
	}
	return true, nil
}

// Create creates a role in the Redpanda cluster with its specified principals.
func (c *Client) Create(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	_, err := c.adminClient.SecurityService().CreateRole(ctx, connect.NewRequest(
		adminv2.CreateRoleRequest_builder{
			Role: adminv2.Role_builder{
				Name:    effectiveRoleName,
				Members: principalsToProtoMembers(role.Spec.Principals),
			}.Build(),
		}.Build(),
	))
	if err != nil {
		return errors.Wrapf(err, "creating role %s", effectiveRoleName)
	}

	return nil
}

// Delete removes a role from the Redpanda cluster.
func (c *Client) Delete(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	_, err := c.adminClient.SecurityService().DeleteRole(ctx, connect.NewRequest(
		adminv2.DeleteRoleRequest_builder{
			Name:       effectiveRoleName,
			DeleteAcls: true,
		}.Build(),
	))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil
		}
		return errors.Wrapf(err, "deleting role %s", effectiveRoleName)
	}
	return nil
}

// Update updates an existing role's membership in the Redpanda cluster.
func (c *Client) Update(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	resp, err := c.adminClient.SecurityService().GetRole(ctx, connect.NewRequest(
		adminv2.GetRoleRequest_builder{Name: effectiveRoleName}.Build(),
	))
	if err != nil {
		return errors.Wrapf(err, "getting role %s", effectiveRoleName)
	}

	currentPrincipals := protoMembersToStringSlice(resp.Msg.GetRole().GetMembers())
	toAdd, toRemove := rolesinternal.CalculateMembershipChanges(currentPrincipals, role.Spec.Principals)

	if len(toAdd) > 0 {
		_, err := c.adminClient.SecurityService().AddRoleMembers(ctx, connect.NewRequest(
			adminv2.AddRoleMembersRequest_builder{
				RoleName: effectiveRoleName,
				Members:  principalsToProtoMembers(toAdd),
			}.Build(),
		))
		if err != nil {
			return errors.Wrapf(err, "adding members to role %s", effectiveRoleName)
		}
	}

	if len(toRemove) > 0 {
		_, err := c.adminClient.SecurityService().RemoveRoleMembers(ctx, connect.NewRequest(
			adminv2.RemoveRoleMembersRequest_builder{
				RoleName: effectiveRoleName,
				Members:  principalsToProtoMembers(toRemove),
			}.Build(),
		))
		if err != nil {
			return errors.Wrapf(err, "removing members from role %s", effectiveRoleName)
		}
	}

	return nil
}

// ClearPrincipals removes all principals from a role.
func (c *Client) ClearPrincipals(ctx context.Context, role *redpandav1alpha2.RedpandaRole) error {
	if err := rolesinternal.ValidateRole(role); err != nil {
		return err
	}

	effectiveRoleName := role.GetEffectiveRoleName()

	resp, err := c.adminClient.SecurityService().GetRole(ctx, connect.NewRequest(
		adminv2.GetRoleRequest_builder{Name: effectiveRoleName}.Build(),
	))
	if err != nil {
		return errors.Wrapf(err, "getting current role members for %s", effectiveRoleName)
	}

	members := resp.Msg.GetRole().GetMembers()
	if len(members) == 0 {
		return nil
	}

	_, err = c.adminClient.SecurityService().RemoveRoleMembers(ctx, connect.NewRequest(
		adminv2.RemoveRoleMembersRequest_builder{
			RoleName: effectiveRoleName,
			Members:  members,
		}.Build(),
	))
	if err != nil {
		return errors.Wrapf(err, "clearing principals for role %s", effectiveRoleName)
	}

	return nil
}

// principalsToProtoMembers converts principal strings to v2 proto RoleMember types.
func principalsToProtoMembers(principals []string) []*adminv2.RoleMember {
	members := make([]*adminv2.RoleMember, len(principals))
	for i, p := range principals {
		parsed := rolesinternal.ParsePrincipal(p)
		member := &adminv2.RoleMember{}
		switch parsed.Type {
		case rolesinternal.PrincipalTypeGroup:
			member.SetGroup(adminv2.RoleGroup_builder{Name: parsed.Name}.Build())
		default:
			member.SetUser(adminv2.RoleUser_builder{Name: parsed.Name}.Build())
		}
		members[i] = member
	}
	return members
}

// protoMembersToStringSlice converts v2 proto RoleMember slice to principal string slice.
func protoMembersToStringSlice(members []*adminv2.RoleMember) []string {
	result := make([]string, 0, len(members))
	for _, m := range members {
		if user := m.GetUser(); user != nil {
			result = append(result, rolesinternal.PrincipalTypeUser+":"+user.GetName())
		} else if group := m.GetGroup(); group != nil {
			result = append(result, rolesinternal.PrincipalTypeGroup+":"+group.GetName())
		}
	}
	return result
}
