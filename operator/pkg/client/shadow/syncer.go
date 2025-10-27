// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package shadow

import (
	"context"
	"encoding/json"
	"errors"

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	"github.com/redpanda-data/common-go/rpadmin"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// Syncer synchronizes Schemas for the given object to Redpanda.
type Syncer struct {
	client *rpadmin.AdminAPI
}

// NewSyncer initializes a Syncer.
func NewSyncer(client *rpadmin.AdminAPI) *Syncer {
	return &Syncer{
		client: client,
	}
}

// Sync synchronizes the shadow link in Redpanda.
func (s *Syncer) Sync(ctx context.Context, o *redpandav1alpha2.ShadowLink, remoteClusterSettings RemoteClusterSettings) (*redpandav1alpha2.ShadowLinkStatus, error) {
	response, err := s.client.ShadowLinkService().GetShadowLink(ctx, connect.NewRequest(&adminv2api.GetShadowLinkRequest{
		Name: o.Name,
	}))
	var existing *adminv2api.GetShadowLinkResponse
	if err != nil {
		var httpError *rpadmin.HTTPResponseError
		if errors.As(err, &httpError) {
			genericErr := struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			}{}

			if decodeErr := json.Unmarshal(httpError.Body, &genericErr); decodeErr != nil {
				return nil, errors.Join(err, decodeErr)
			}
			// on a 404, we don't error
			if genericErr.Code != "not_found" {
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		existing = response.Msg
	}

	// creation
	if existing == nil {
		response, err := s.client.ShadowLinkService().CreateShadowLink(ctx, connect.NewRequest(&adminv2api.CreateShadowLinkRequest{
			ShadowLink: convertCRDToAPIShadowLink(o, remoteClusterSettings),
		}))
		if err != nil {
			return nil, err
		}

		converted := convertAPIToCRDStatus(response.Msg.ShadowLink.Status)
		return ptr.To(converted), nil
	}

	update, err := s.client.ShadowLinkService().UpdateShadowLink(ctx, connect.NewRequest(&adminv2api.UpdateShadowLinkRequest{
		ShadowLink: convertCRDToAPIShadowLink(o, remoteClusterSettings),
		UpdateMask: &fieldmaskpb.FieldMask{
			// From: https://github.com/redpanda-data/redpanda/blob/60c590be34d5b2bd2934ac2143105ee7e2442388/src/v/redpanda/admin/services/shadow_link/shadow_link.cc#L64C1-L66C57
			// "configurations", "client_options", "bootstrap_servers"
			// "configurations", "client_options", "tls_settings"
			//
			// update all fields
			Paths: []string{"configurations.client_options.authentication_configuration", "configurations.topic_metadata_sync_options", "configurations.consumer_offset_sync_options", "configurations.security_sync_options"},
		},
	}))
	if err != nil {
		return nil, err
	}
	converted := convertAPIToCRDStatus(update.Msg.ShadowLink.Status)
	return ptr.To(converted), nil
}

// Delete deletes the shadow link in Redpanda.
func (s *Syncer) Delete(ctx context.Context, o *redpandav1alpha2.ShadowLink) error {
	_, err := s.client.ShadowLinkService().DeleteShadowLink(ctx, connect.NewRequest(&adminv2api.DeleteShadowLinkRequest{
		Name:  o.Name,
		Force: true,
	}))

	return err
}

// Close closes the underlying client connection.
func (s *Syncer) Close() {
	s.client.Close()
}
