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
	"github.com/redpanda-data/common-go/rpsr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

var (
	resourceTypeToSR = map[redpandav1alpha2.ResourceType]rpsr.ResourceType{
		redpandav1alpha2.ResourceTypeSchemaRegistrySubject:  rpsr.ResourceTypeSubject,
		redpandav1alpha2.ResourceTypeSchemaRegistryRegistry: rpsr.ResourceTypeRegistry,
	}
	patternTypeToSR = map[redpandav1alpha2.PatternType]rpsr.PatternType{
		redpandav1alpha2.PatternTypeLiteral:  rpsr.PatternTypeLiteral,
		redpandav1alpha2.PatternTypePrefixed: rpsr.PatternTypePrefix,
	}
	operationToSR = map[redpandav1alpha2.ACLOperation]rpsr.Operation{
		redpandav1alpha2.ACLOperationRead:            rpsr.OperationRead,
		redpandav1alpha2.ACLOperationWrite:           rpsr.OperationWrite,
		redpandav1alpha2.ACLOperationDelete:          rpsr.OperationDelete,
		redpandav1alpha2.ACLOperationDescribe:        rpsr.OperationDescribe,
		redpandav1alpha2.ACLOperationDescribeConfigs: rpsr.OperationDescribeConfig,
		redpandav1alpha2.ACLOperationAlterConfigs:    rpsr.OperationAlterConfig,
	}
	permissionToSR = map[redpandav1alpha2.ACLType]rpsr.Permission{
		redpandav1alpha2.ACLTypeAllow: rpsr.PermissionAllow,
		redpandav1alpha2.ACLTypeDeny:  rpsr.PermissionDeny,
	}
)

func resourceTypeToSRACL(t redpandav1alpha2.ResourceType) rpsr.ResourceType {
	return resourceTypeToSR[t]
}

func patternTypeToSRACL(p redpandav1alpha2.PatternType) rpsr.PatternType {
	if pt, ok := patternTypeToSR[p]; ok {
		return pt
	}
	return rpsr.PatternTypeLiteral
}

func operationToSRACL(op redpandav1alpha2.ACLOperation) rpsr.Operation {
	return operationToSR[op]
}

func permissionToSRACL(t redpandav1alpha2.ACLType) rpsr.Permission {
	return permissionToSR[t]
}

func aclsFromACLRule(principal string, r redpandav1alpha2.ACLRule) []rpsr.ACL {
	var acls []rpsr.ACL
	for _, op := range r.Operations {
		acls = append(acls, rpsr.ACL{
			Principal:    principal,
			Resource:     r.Resource.GetName(),
			ResourceType: resourceTypeToSRACL(r.Resource.Type),
			PatternType:  patternTypeToSRACL(r.Resource.GetPatternType()),
			Host:         r.GetHost(),
			Operation:    operationToSRACL(op),
			Permission:   permissionToSRACL(r.Type),
		})
	}
	return acls
}

// aclKey is a comparable struct used for set-based diffing of SR ACLs.
type aclKey struct {
	Principal    string
	Resource     string
	ResourceType rpsr.ResourceType
	PatternType  rpsr.PatternType
	Host         string
	Operation    rpsr.Operation
	Permission   rpsr.Permission
}

func keyFromACL(acl rpsr.ACL) aclKey {
	return aclKey{
		Principal:    acl.Principal,
		Resource:     acl.Resource,
		ResourceType: acl.ResourceType,
		PatternType:  acl.PatternType,
		Host:         acl.Host,
		Operation:    acl.Operation,
		Permission:   acl.Permission,
	}
}

func aclFromKey(k aclKey) rpsr.ACL {
	return rpsr.ACL{
		Principal:    k.Principal,
		Resource:     k.Resource,
		ResourceType: k.ResourceType,
		PatternType:  k.PatternType,
		Host:         k.Host,
		Operation:    k.Operation,
		Permission:   k.Permission,
	}
}

func calculateSRACLs(principal string, desired []redpandav1alpha2.ACLRule, existing []rpsr.ACL) (toCreate, toDelete []rpsr.ACL) {
	desiredKeys := collections.NewSet[aclKey]()
	for _, r := range desired {
		for _, acl := range aclsFromACLRule(principal, r) {
			desiredKeys.Add(keyFromACL(acl))
		}
	}

	existingKeys := collections.NewSet[aclKey]()
	for _, acl := range existing {
		existingKeys.Add(keyFromACL(acl))
	}

	toCreate = collections.MapSet(desiredKeys.LeftDisjoint(existingKeys), aclFromKey)
	toDelete = collections.MapSet(desiredKeys.RightDisjoint(existingKeys), aclFromKey)

	return toCreate, toDelete
}
