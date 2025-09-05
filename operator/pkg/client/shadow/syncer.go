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

	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/client/shadow/adminv2"
)

// Syncer synchronizes Schemas for the given object to Redpanda.
type Syncer struct {
	client *adminv2.Client
}

// NewSyncer initializes a Syncer.
func NewSyncer(client *adminv2.Client) *Syncer {
	return &Syncer{
		client: client,
	}
}

// Sync synchronizes the shadow link in Redpanda.
func (s *Syncer) Sync(ctx context.Context, o *redpandav1alpha2.ShadowLink, remoteClusterSettings RemoteClusterSettings) ([]redpandav1alpha2.ShadowLinkTaskStatus, []redpandav1alpha2.ShadowTopicStatus, error) {
	response, err := s.client.ShadowLinks().CreateShadowLink(ctx, connect.NewRequest(&adminv2api.CreateShadowLinkRequest{
		ShadowLink: convertCRDToAPIShadowLink(o, remoteClusterSettings),
	}))
	if err != nil {
		return nil, nil, err
	}

	converted := convertAPIToCRDStatus(response.Msg.ShadowLink.Status)
	return converted.TaskStatuses, converted.ShadowTopicStatuses, nil
}

// Delete deletes the shadow link in Redpanda.
func (s *Syncer) Delete(ctx context.Context, o *redpandav1alpha2.ShadowLink) error {
	_, err := s.client.ShadowLinks().DeleteShadowLink(ctx, connect.NewRequest(&adminv2api.DeleteShadowLinkRequest{
		Name: o.Name,
	}))
	return err
}

// Close closes the underlying client connection.
func (s *Syncer) Close() {
	s.client.Close()
}
