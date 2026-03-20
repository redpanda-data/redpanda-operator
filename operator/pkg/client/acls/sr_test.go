// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package acls

import (
	"testing"

	"github.com/redpanda-data/common-go/rpsr"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestAclsFromACLRule(t *testing.T) {
	t.Run("subject with single operation", func(t *testing.T) {
		rule := redpandav1alpha2.ACLRule{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name: "my-subject",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
			},
		}

		acls := aclsFromACLRule("User:alice", rule)
		require.Len(t, acls, 1)
		require.Equal(t, rpsr.ACL{
			Principal:    "User:alice",
			Resource:     "my-subject",
			ResourceType: rpsr.ResourceTypeSubject,
			PatternType:  rpsr.PatternTypeLiteral,
			Host:         "*",
			Operation:    rpsr.OperationRead,
			Permission:   rpsr.PermissionAllow,
		}, acls[0])
	})

	t.Run("registry resource type maps to REGISTRY", func(t *testing.T) {
		rule := redpandav1alpha2.ACLRule{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistryRegistry,
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
			},
		}

		acls := aclsFromACLRule("User:alice", rule)
		require.Len(t, acls, 1)
		require.Equal(t, rpsr.ResourceTypeRegistry, acls[0].ResourceType)
	})

	t.Run("multiple operations expand to multiple rules", func(t *testing.T) {
		rule := redpandav1alpha2.ACLRule{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name: "my-subject",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
				redpandav1alpha2.ACLOperationWrite,
				redpandav1alpha2.ACLOperationDelete,
			},
		}

		acls := aclsFromACLRule("User:alice", rule)
		require.Len(t, acls, 3)

		ops := map[rpsr.Operation]bool{}
		for _, a := range acls {
			ops[a.Operation] = true
			require.Equal(t, "User:alice", a.Principal)
			require.Equal(t, rpsr.ResourceTypeSubject, a.ResourceType)
			require.Equal(t, rpsr.PermissionAllow, a.Permission)
		}
		require.True(t, ops[rpsr.OperationRead])
		require.True(t, ops[rpsr.OperationWrite])
		require.True(t, ops[rpsr.OperationDelete])
	})

	t.Run("prefixed pattern type", func(t *testing.T) {
		rule := redpandav1alpha2.ACLRule{
			Type: redpandav1alpha2.ACLTypeDeny,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type:        redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name:        "prefix-",
				PatternType: ptr.To(redpandav1alpha2.PatternTypePrefixed),
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationWrite,
			},
		}

		acls := aclsFromACLRule("RedpandaRole:myrole", rule)
		require.Len(t, acls, 1)
		require.Equal(t, rpsr.PatternTypePrefix, acls[0].PatternType)
		require.Equal(t, rpsr.PermissionDeny, acls[0].Permission)
		require.Equal(t, "RedpandaRole:myrole", acls[0].Principal)
	})
}

func TestCalculateSRACLs(t *testing.T) {
	t.Run("empty desired and empty existing", func(t *testing.T) {
		toCreate, toDelete := calculateSRACLs("User:alice", nil, nil)
		require.Empty(t, toCreate)
		require.Empty(t, toDelete)
	})

	t.Run("creates only", func(t *testing.T) {
		desired := []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name: "my-subject",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
			},
		}}

		toCreate, toDelete := calculateSRACLs("User:alice", desired, nil)
		require.Len(t, toCreate, 1)
		require.Empty(t, toDelete)
		require.Equal(t, "my-subject", toCreate[0].Resource)
		require.Equal(t, rpsr.OperationRead, toCreate[0].Operation)
	})

	t.Run("deletes only", func(t *testing.T) {
		existing := []rpsr.ACL{{
			Principal:    "User:alice",
			Resource:     "old-subject",
			ResourceType: rpsr.ResourceTypeSubject,
			PatternType:  rpsr.PatternTypeLiteral,
			Host:         "*",
			Operation:    rpsr.OperationRead,
			Permission:   rpsr.PermissionAllow,
		}}

		toCreate, toDelete := calculateSRACLs("User:alice", nil, existing)
		require.Empty(t, toCreate)
		require.Len(t, toDelete, 1)
		require.Equal(t, "old-subject", toDelete[0].Resource)
	})

	t.Run("mixed creates and deletes", func(t *testing.T) {
		desired := []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name: "new-subject",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
			},
		}}

		existing := []rpsr.ACL{{
			Principal:    "User:alice",
			Resource:     "old-subject",
			ResourceType: rpsr.ResourceTypeSubject,
			PatternType:  rpsr.PatternTypeLiteral,
			Host:         "*",
			Operation:    rpsr.OperationRead,
			Permission:   rpsr.PermissionAllow,
		}}

		toCreate, toDelete := calculateSRACLs("User:alice", desired, existing)
		require.Len(t, toCreate, 1)
		require.Len(t, toDelete, 1)
		require.Equal(t, "new-subject", toCreate[0].Resource)
		require.Equal(t, "old-subject", toDelete[0].Resource)
	})

	t.Run("idempotent when desired matches existing", func(t *testing.T) {
		desired := []redpandav1alpha2.ACLRule{{
			Type: redpandav1alpha2.ACLTypeAllow,
			Resource: redpandav1alpha2.ACLResourceSpec{
				Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
				Name: "my-subject",
			},
			Operations: []redpandav1alpha2.ACLOperation{
				redpandav1alpha2.ACLOperationRead,
			},
		}}

		existing := []rpsr.ACL{{
			Principal:    "User:alice",
			Resource:     "my-subject",
			ResourceType: rpsr.ResourceTypeSubject,
			PatternType:  rpsr.PatternTypeLiteral,
			Host:         "*",
			Operation:    rpsr.OperationRead,
			Permission:   rpsr.PermissionAllow,
		}}

		toCreate, toDelete := calculateSRACLs("User:alice", desired, existing)
		require.Empty(t, toCreate)
		require.Empty(t, toDelete)
	})

	t.Run("deduplication of desired rules", func(t *testing.T) {
		desired := []redpandav1alpha2.ACLRule{
			{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
					Name: "my-subject",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			},
			{
				Type: redpandav1alpha2.ACLTypeAllow,
				Resource: redpandav1alpha2.ACLResourceSpec{
					Type: redpandav1alpha2.ResourceTypeSchemaRegistrySubject,
					Name: "my-subject",
				},
				Operations: []redpandav1alpha2.ACLOperation{
					redpandav1alpha2.ACLOperationRead,
				},
			},
		}

		toCreate, toDelete := calculateSRACLs("User:alice", desired, nil)
		require.Len(t, toCreate, 1)
		require.Empty(t, toDelete)
	})
}
