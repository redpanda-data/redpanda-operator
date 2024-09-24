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
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kmsg"
	"k8s.io/utils/ptr"
)

type rule struct {
	ResourceType        kmsg.ACLResourceType
	ResourceName        string
	ResourcePatternType kmsg.ACLResourcePatternType
	Principal           string
	Host                string
	Operation           kmsg.ACLOperation
	PermissionType      kmsg.ACLPermissionType
}

func rulesFromV1Alpha2ACL(principal string, r redpandav1alpha2.ACLRule) ([]rule, error) { //nolint:gocritic // pass by value here is fine
	rules := []rule{}
	for _, operation := range r.Operations {
		rules = append(rules, rule{
			Principal:           principal,
			ResourceType:        r.Resource.Type.ToKafka(),
			ResourceName:        r.Resource.GetName(),
			ResourcePatternType: r.Resource.PatternType.ToKafka(),
			Host:                r.GetHost(),
			Operation:           operation.ToKafka(),
			PermissionType:      r.Type.ToKafka(),
		})
	}

	return rules, nil
}

func ruleToV1Alpha2Rule(r rule) redpandav1alpha2.ACLRule {
	return redpandav1alpha2.ACLRule{
		Type: redpandav1alpha2.ACLTypeFromKafka(r.PermissionType),
		Resource: redpandav1alpha2.ACLResourceSpec{
			Type:        redpandav1alpha2.ResourceTypeFromKafka(r.ResourceType),
			Name:        r.ResourceName,
			PatternType: ptr.To(redpandav1alpha2.ACLPatternTypeFromKafka(r.ResourcePatternType)),
		},
		Host: &r.Host,
		Operations: []redpandav1alpha2.ACLOperation{
			redpandav1alpha2.ACLOperationFromKafka(r.Operation),
		},
	}
}

func ruleToDeletionFilter(r rule) kmsg.DeleteACLsRequestFilter {
	return kmsg.DeleteACLsRequestFilter{
		ResourceType:        r.ResourceType,
		ResourceName:        &r.ResourceName,
		ResourcePatternType: r.ResourcePatternType,
		Principal:           &r.Principal,
		Host:                &r.Host,
		Operation:           r.Operation,
		PermissionType:      r.PermissionType,
	}
}

func ruleToCreationRequest(r rule) kmsg.CreateACLsRequestCreation {
	return kmsg.CreateACLsRequestCreation{
		ResourceType:        r.ResourceType,
		ResourceName:        r.ResourceName,
		ResourcePatternType: r.ResourcePatternType,
		Principal:           r.Principal,
		Host:                r.Host,
		Operation:           r.Operation,
		PermissionType:      r.PermissionType,
	}
}
