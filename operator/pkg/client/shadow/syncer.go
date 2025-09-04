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

	"github.com/redpanda-data/common-go/rpadmin"
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
func (s *Syncer) Sync(ctx context.Context, o *redpandav1alpha2.ShadowLink, remoteClusterSettings any) ([]redpandav1alpha2.ShadowLinkTaskStatus, []redpandav1alpha2.ShadowTopicStatus, error) {
	return nil, nil, nil
}

// Delete deletes the shadow link in Redpanda.
func (s *Syncer) Delete(ctx context.Context, o *redpandav1alpha2.ShadowLink) error {
	return nil
}

// Close closes the underlying client connection.
func (s *Syncer) Close() {
	s.client.Close()
}
