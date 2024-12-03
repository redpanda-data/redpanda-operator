// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package acls

import (
	"github.com/twmb/franz-go/pkg/kmsg"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

func rulesetFromDescribeResponse(acls []kmsg.DescribeACLsResponseResource) collections.Set[rule] {
	rules := collections.NewSet[rule]()

	if acls == nil {
		return rules
	}

	for _, resource := range acls {
		for _, acl := range resource.ACLs {
			rules.Add(rule{
				ResourceType:        resource.ResourceType,
				ResourceName:        resource.ResourceName,
				ResourcePatternType: resource.ResourcePatternType,
				Principal:           acl.Principal,
				Host:                acl.Host,
				Operation:           acl.Operation,
				PermissionType:      acl.PermissionType,
			})
		}
	}

	return rules
}

func calculateACLs(principal string, rules []redpandav1alpha2.ACLRule, existing []kmsg.DescribeACLsResponseResource) ([]kmsg.CreateACLsRequestCreation, []kmsg.DeleteACLsRequestFilter) {
	// initially mark all existing acls as needing deletion
	existingRules := rulesetFromDescribeResponse(existing)
	desiredRules := collections.NewSet[rule]()

	// now regenerate the acls, removing any that should still
	// exist from our deletion set and adding them to the creation set
	// if they don't yet exist
	for _, rule := range rules {
		rules := rulesFromV1Alpha2ACL(principal, rule)

		for _, acl := range rules {
			desiredRules.Add(acl)
		}
	}

	toCreate := collections.MapSet(desiredRules.LeftDisjoint(existingRules), ruleToCreationRequest)
	toDelete := collections.MapSet(desiredRules.RightDisjoint(existingRules), ruleToDeletionFilter)

	return toCreate, toDelete
}
