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
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type ruleset map[rule]struct{}

func (s ruleset) has(r rule) bool {
	_, ok := s[r]
	return ok
}

func (s ruleset) delete(r rule) {
	delete(s, r)
}

func (s ruleset) add(r rule) {
	s[r] = struct{}{}
}

func (s ruleset) clone() ruleset {
	set := ruleset{}
	for r := range s {
		set[r] = struct{}{}
	}
	return set
}

func (s ruleset) asV1Alpha2Rules() []redpandav1alpha2.ACLRule {
	rules := []redpandav1alpha2.ACLRule{}
	for rule := range s {
		rules = append(rules, rule.toV1Alpha2Rule())
	}
	return rules
}

func (s ruleset) asDeletionFilters() []kmsg.DeleteACLsRequestFilter {
	filters := []kmsg.DeleteACLsRequestFilter{}
	for rule := range s {
		filters = append(filters, rule.toDeletionFilter())
	}
	return filters
}

func (s ruleset) asCreationRequests() []kmsg.CreateACLsRequestCreation {
	creations := []kmsg.CreateACLsRequestCreation{}
	for rule := range s {
		creations = append(creations, rule.toCreationRequest())
	}
	return creations
}

func rulesetFromDescribeResponse(acls []kmsg.DescribeACLsResponseResource) ruleset {
	rules := ruleset{}

	if acls == nil {
		return rules
	}

	for _, resource := range acls {
		for _, acl := range resource.ACLs {
			rules[rule{
				ResourceType:        resource.ResourceType,
				ResourceName:        resource.ResourceName,
				ResourcePatternType: resource.ResourcePatternType,
				Principal:           acl.Principal,
				Host:                acl.Host,
				Operation:           acl.Operation,
				PermissionType:      acl.PermissionType,
			}] = struct{}{}
		}
	}

	return rules
}

func calculateACLs(principal string, rules []redpandav1alpha2.ACLRule, existing []kmsg.DescribeACLsResponseResource) ([]kmsg.CreateACLsRequestCreation, []kmsg.DeleteACLsRequestFilter, error) {
	// initially mark all existing acls as needing deletion
	toDelete := rulesetFromDescribeResponse(existing)
	toCreate := ruleset{}

	// exists is to keep track of any acls that already exist,
	// so we don't need to add them to the creation set
	exists := toDelete.clone()

	// now regenerate the acls, removing any that should still
	// exist from our deletion set and adding them to the creation set
	// if they don't yet exist
	for _, rule := range rules {
		rules, err := rulesFromV1Alpha2ACL(principal, rule)
		if err != nil {
			return nil, nil, err
		}

		for _, acl := range rules {
			toDelete.delete(acl)
			if !exists.has(acl) {
				toCreate.add(acl)
			}
		}
	}

	return toCreate.asCreationRequests(), toDelete.asDeletionFilters(), nil
}
