package client

import (
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kmsg"
	"k8s.io/utils/ptr"
)

type aclRule struct {
	ResourceType        kmsg.ACLResourceType
	ResourceName        string
	ResourcePatternType kmsg.ACLResourcePatternType
	Principal           string
	Host                string
	Operation           kmsg.ACLOperation
	PermissionType      kmsg.ACLPermissionType
}

func aclRulesFromUserACL(principal string, rule redpandav1alpha2.ACLRule) ([]aclRule, error) {
	rules := []aclRule{}

	resourceType, err := rule.Resource.Type.ToKafka()
	if err != nil {
		return nil, err
	}

	permType, err := rule.Type.ToKafka()
	if err != nil {
		return nil, err
	}

	patternType, err := rule.Resource.PatternType.ToKafka()
	if err != nil {
		return nil, err
	}

	for _, operation := range rule.Operations {
		op, err := operation.ToKafka()
		if err != nil {
			return nil, err
		}

		rules = append(rules, aclRule{
			Principal:           principal,
			ResourceType:        resourceType,
			ResourceName:        rule.Resource.Name,
			ResourcePatternType: patternType,
			Host:                ptr.Deref(rule.Host, "*"),
			Operation:           op,
			PermissionType:      permType,
		})
	}

	return rules, nil
}

func (r aclRule) toDeletionFilter() kmsg.DeleteACLsRequestFilter {
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

func (r aclRule) toCreationRequest() kmsg.CreateACLsRequestCreation {
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

type aclRuleset map[aclRule]struct{}

func (s aclRuleset) Flatten() []aclRule {
	rules := []aclRule{}
	for rule := range s {
		rules = append(rules, rule)
	}
	return rules
}

func (s aclRuleset) Has(rule aclRule) bool {
	_, ok := s[rule]
	return ok
}

func (s aclRuleset) Delete(rule aclRule) {
	delete(s, rule)
}

func (s aclRuleset) Add(rule aclRule) {
	s[rule] = struct{}{}
}

func (s aclRuleset) Clone() aclRuleset {
	set := aclRuleset{}
	for r := range s {
		set[r] = struct{}{}
	}
	return set
}

func (s aclRuleset) AsDeletions() []kmsg.DeleteACLsRequestFilter {
	filters := []kmsg.DeleteACLsRequestFilter{}
	for rule := range s {
		filters = append(filters, rule.toDeletionFilter())
	}
	return filters
}

func (s aclRuleset) AsCreations() []kmsg.CreateACLsRequestCreation {
	creations := []kmsg.CreateACLsRequestCreation{}
	for rule := range s {
		creations = append(creations, rule.toCreationRequest())
	}
	return creations
}

func aclRuleSetFromDescribeResponse(acls []kmsg.DescribeACLsResponseResource) aclRuleset {
	rules := aclRuleset{}

	if acls == nil {
		return rules
	}

	for _, resource := range acls {
		for _, acl := range resource.ACLs {
			rules[aclRule{
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

func calculateACLs(user *redpandav1alpha2.User, acls []kmsg.DescribeACLsResponseResource) ([]kmsg.CreateACLsRequestCreation, []kmsg.DeleteACLsRequestFilter, error) {
	// initially mark all existing acls as needing deletion
	toDelete := aclRuleSetFromDescribeResponse(acls)
	toCreate := aclRuleset{}

	// exists is to keep track of any acls that already exist,
	// so we don't need to add them to the creation set
	exists := toDelete.Clone()

	// now regenerate the acls, removing any that should still
	// exist from our deletion set and adding them to the creation set
	// if they don't yet exist
	for _, rule := range user.Spec.Authorization.ACLs {
		rules, err := aclRulesFromUserACL(user.ACLName(), rule)
		if err != nil {
			return nil, nil, err
		}

		for _, acl := range rules {
			toDelete.Delete(acl)
			if !exists.Has(acl) {
				toCreate.Add(acl)
			}
		}
	}

	return toCreate.AsCreations(), toDelete.AsDeletions(), nil
}
