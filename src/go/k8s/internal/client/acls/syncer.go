package acls

import (
	"context"
	"errors"
	"fmt"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Syncer synchronizes ACLs for the given object to Redpanda.
type Syncer struct {
	client *kgo.Client
}

// NewSyncer initializes a Syncer.
func NewSyncer(client *kgo.Client) *Syncer {
	return &Syncer{
		client: client,
	}
}

// DeleteAll removes all ACLs for the object in Redpanda.
func (s *Syncer) DeleteAll(ctx context.Context, o redpandav1alpha2.AuthorizedObject) error {
	return s.deleteAll(ctx, o.GetPrincipal())
}

// Sync synchronizes all ACLs for the given object to Redpanda, deleting
// any additional ACLs that were found, and creating any that need to be created.
func (s *Syncer) Sync(ctx context.Context, o redpandav1alpha2.AuthorizedObject) error {
	_, _, err := s.sync(ctx, o.GetPrincipal(), o.GetACLs())
	return err
}

func (s *Syncer) deleteAll(ctx context.Context, principal string) error {
	ptrUsername := kmsg.StringPtr(principal)

	req := kmsg.NewPtrDeleteACLsRequest()
	req.Filters = []kmsg.DeleteACLsRequestFilter{{
		PermissionType:      kmsg.ACLPermissionTypeAny,
		ResourceType:        kmsg.ACLResourceTypeAny,
		ResourcePatternType: kmsg.ACLResourcePatternTypeAny,
		Principal:           ptrUsername,
		Operation:           kmsg.ACLOperationAny,
	}}

	response, err := req.RequestWith(ctx, s.client)
	if err != nil {
		return err
	}

	for _, result := range response.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}

		// we have an error code but no error message
		if result.ErrorCode != 0 {
			return fmt.Errorf("error code: %d", result.ErrorCode)
		}
	}

	return nil
}

func (s *Syncer) sync(ctx context.Context, principal string, rules []redpandav1alpha2.ACLRule) (int, int, error) {
	acls, err := s.listACLs(ctx, principal)
	if err != nil {
		return 0, 0, err
	}

	creations, deletions, err := calculateACLs(principal, rules, acls)
	if err != nil {
		return 0, 0, err
	}

	if err := s.createACLs(ctx, creations); err != nil {
		return 0, 0, err
	}
	if err := s.deleteACLs(ctx, deletions); err != nil {
		return 0, 0, err
	}

	return len(creations), len(deletions), nil
}

func (s *Syncer) listACLs(ctx context.Context, principal string) ([]kmsg.DescribeACLsResponseResource, error) {
	ptrUsername := kmsg.StringPtr(principal)

	req := kmsg.NewPtrDescribeACLsRequest()
	req.PermissionType = kmsg.ACLPermissionTypeAny
	req.ResourceType = kmsg.ACLResourceTypeAny
	req.Principal = ptrUsername
	req.Operation = kmsg.ACLOperationAny

	response, err := req.RequestWith(ctx, s.client)
	if err != nil {
		return nil, err
	}
	if response.ErrorMessage != nil {
		return nil, errors.New(*response.ErrorMessage)
	}

	// we have an error code but no error message
	if response.ErrorCode != 0 {
		return nil, fmt.Errorf("error code: %d", response.ErrorCode)
	}

	return response.Resources, nil
}

func (s *Syncer) createACLs(ctx context.Context, acls []kmsg.CreateACLsRequestCreation) error {
	if len(acls) == 0 {
		return nil
	}

	req := kmsg.NewPtrCreateACLsRequest()
	req.Creations = acls

	creation, err := req.RequestWith(ctx, s.client)
	if err != nil {
		return err
	}

	for _, result := range creation.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}

		// we have an error code but no error message
		if result.ErrorCode != 0 {
			return fmt.Errorf("error code: %d", result.ErrorCode)
		}
	}

	return nil
}

func (s *Syncer) deleteACLs(ctx context.Context, deletions []kmsg.DeleteACLsRequestFilter) error {
	if len(deletions) == 0 {
		return nil
	}

	req := kmsg.NewPtrDeleteACLsRequest()
	req.Filters = deletions

	response, err := req.RequestWith(ctx, s.client)
	if err != nil {
		return err
	}

	for _, result := range response.Results {
		if result.ErrorMessage != nil {
			return errors.New(*result.ErrorMessage)
		}
	}

	return nil
}
