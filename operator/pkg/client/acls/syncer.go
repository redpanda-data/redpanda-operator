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
	"context"
	"errors"
	"fmt"

	"github.com/redpanda-data/common-go/rpsr"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/collections"
)

// ErrSchemaRegistryNotConfigured is returned by Sync when the object contains
// Schema Registry ACL rules but no SR client was provided. Kafka ACLs are
// still synced successfully; callers can check for this error to report a
// partial sync to the user.
var ErrSchemaRegistryNotConfigured = errors.New("schema registry ACL client not configured; schema registry ACLs were not synced")

// Syncer synchronizes ACLs for the given object to Redpanda.
type Syncer struct {
	client   *kgo.Client
	srClient rpsr.ACLClient
}

// NewSyncer initializes a Syncer. client must be non-nil; srClient is optional.
func NewSyncer(client *kgo.Client, srClient rpsr.ACLClient) *Syncer {
	if client == nil {
		panic("kafka client must be non-nil")
	}
	return &Syncer{
		client:   client,
		srClient: srClient,
	}
}

// Sync synchronizes all ACLs for the given object to Redpanda, deleting
// any additional ACLs that were found, and creating any that need to be created.
// Rules are partitioned by resource type: Kafka resource types go through the
// Kafka protocol, Schema Registry resource types go through the SR HTTP API.
//
// Kafka ACLs are always synced first. If the object contains Schema Registry
// ACL rules but no SR client was provided, Sync returns
// ErrSchemaRegistryNotConfigured after the Kafka ACLs have already been
// applied. Callers should check for this sentinel error (via errors.Is) to
// detect and report the partial synchronization to the user.
func (s *Syncer) Sync(ctx context.Context, o redpandav1alpha2.AuthorizedObject) error {
	kafkaRules, srRules := redpandav1alpha2.PartitionACLs(o.GetACLs())

	if _, _, err := s.syncACL(ctx, o.GetPrincipal(), kafkaRules); err != nil {
		return err
	}

	if len(srRules) > 0 && s.srClient == nil {
		return ErrSchemaRegistryNotConfigured
	}

	if s.srClient != nil {
		if _, _, err := s.syncSRACL(ctx, o.GetPrincipal(), srRules); err != nil {
			return err
		}
	}

	return nil
}

// DeleteAll removes all ACLs for the object in Redpanda.
func (s *Syncer) DeleteAll(ctx context.Context, o redpandav1alpha2.AuthorizedObject) error {
	if err := s.deleteAllACL(ctx, o.GetPrincipal()); err != nil {
		return err
	}
	if s.srClient != nil {
		if err := s.deleteAllSRACL(ctx, o.GetPrincipal()); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the underlying client connections.
func (s *Syncer) Close() {
	s.client.Close()
}

func (s *Syncer) syncACL(ctx context.Context, principal string, rules []redpandav1alpha2.ACLRule) (created, deleted int, err error) {
	acls, err := s.listACLs(ctx, principal)
	if err != nil {
		return 0, 0, fmt.Errorf("listing ACLs: %w", err)
	}

	creations, deletions := calculateACLs(principal, rules, acls)

	if err := s.createACLs(ctx, creations); err != nil {
		return 0, 0, fmt.Errorf("creating ACLs: %w", err)
	}
	if err := s.deleteACLs(ctx, deletions); err != nil {
		return 0, 0, fmt.Errorf("deleting ACLs: %w", err)
	}

	return len(creations), len(deletions), nil
}

//nolint:unparam // signature matches syncACL for consistency
func (s *Syncer) syncSRACL(ctx context.Context, principal string, rules []redpandav1alpha2.ACLRule) (created, deleted int, err error) {
	existing, err := s.srClient.ListACLs(ctx, &rpsr.ACL{Principal: principal})
	if err != nil {
		return 0, 0, fmt.Errorf("listing SR ACLs: %w", err)
	}

	toCreate, toDelete := calculateSRACLs(principal, rules, existing)

	if len(toCreate) > 0 {
		if err := s.srClient.CreateACLs(ctx, toCreate); err != nil {
			return 0, 0, fmt.Errorf("creating SR ACLs: %w", err)
		}
	}
	if len(toDelete) > 0 {
		if err := s.srClient.DeleteACLs(ctx, toDelete); err != nil {
			return 0, 0, fmt.Errorf("deleting SR ACLs: %w", err)
		}
	}

	return len(toCreate), len(toDelete), nil
}

func (s *Syncer) deleteAllACL(ctx context.Context, principal string) error {
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
		return fmt.Errorf("deleting all ACLs: %w", err)
	}

	for _, result := range response.Results {
		if err := checkError(result.ErrorMessage, result.ErrorCode); err != nil {
			return err
		}
	}

	return nil
}

func (s *Syncer) deleteAllSRACL(ctx context.Context, principal string) error {
	existing, err := s.srClient.ListACLs(ctx, &rpsr.ACL{Principal: principal})
	if err != nil {
		return fmt.Errorf("listing SR ACLs: %w", err)
	}

	if err := s.srClient.DeleteACLs(ctx, existing); err != nil {
		return fmt.Errorf("deleting SR ACLs: %w", err)
	}

	return nil
}

// ListACLs returns all Kafka ACLs for the given principal as v1alpha2 rules.
func (s *Syncer) ListACLs(ctx context.Context, principal string) ([]redpandav1alpha2.ACLRule, error) {
	describeResponse, err := s.listACLs(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("listing ACLs: %w", err)
	}

	rules := collections.MapSet(rulesetFromDescribeResponse(describeResponse), ruleToV1Alpha2Rule)

	return rules, nil
}

func (s *Syncer) listACLs(ctx context.Context, principal string) ([]kmsg.DescribeACLsResponseResource, error) {
	ptrUsername := kmsg.StringPtr(principal)

	req := kmsg.NewPtrDescribeACLsRequest()
	req.PermissionType = kmsg.ACLPermissionTypeAny
	req.ResourceType = kmsg.ACLResourceTypeAny
	req.Principal = ptrUsername
	req.Operation = kmsg.ACLOperationAny
	req.ResourcePatternType = kmsg.ACLResourcePatternTypeAny

	response, err := req.RequestWith(ctx, s.client)
	if err != nil {
		return nil, err
	}

	if err := checkError(response.ErrorMessage, response.ErrorCode); err != nil {
		return nil, err
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
		if err := checkError(result.ErrorMessage, result.ErrorCode); err != nil {
			return err
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
		if err := checkError(result.ErrorMessage, result.ErrorCode); err != nil {
			return err
		}
	}

	return nil
}

func checkError(message *string, code int16) error {
	var errMessage string
	if message != nil {
		errMessage = "Error: " + *message + "; "
	}

	if code != 0 {
		return fmt.Errorf("%s%w", errMessage, kerr.ErrorForCode(code))
	}

	return nil
}
