// Copyright 2026 Redpanda Data, Inc.
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
	"errors"
	"testing"

	"buf.build/gen/go/redpandadata/core/connectrpc/go/redpanda/core/admin/v2/adminv2connect"
	adminv2api "buf.build/gen/go/redpandadata/core/protocolbuffers/go/redpanda/core/admin/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// fakeShadowLinkService is a ShadowLinkServiceClient stub. Only the methods
// exercised by Syncer.Sync's update path are implemented; the rest fall
// through to the embedded nil interface and would panic if called, which is
// what we want (an unexpected call should fail the test loudly).
type fakeShadowLinkService struct {
	adminv2connect.ShadowLinkServiceClient

	// updateMasks records the UpdateMask paths of every UpdateShadowLink call
	// in order, so a test can assert both how many updates were attempted and
	// which mask each used.
	updateMasks [][]string
	// updateResults returns the response/error for the Nth (1-indexed)
	// UpdateShadowLink call.
	updateResults func(attempt int) (*connect.Response[adminv2api.UpdateShadowLinkResponse], error)
}

func (f *fakeShadowLinkService) GetShadowLink(_ context.Context, _ *connect.Request[adminv2api.GetShadowLinkRequest]) (*connect.Response[adminv2api.GetShadowLinkResponse], error) {
	// A non-nil Msg makes Sync treat the link as already existing and take the
	// update (not create) path.
	return connect.NewResponse(&adminv2api.GetShadowLinkResponse{}), nil
}

func (f *fakeShadowLinkService) UpdateShadowLink(_ context.Context, req *connect.Request[adminv2api.UpdateShadowLinkRequest]) (*connect.Response[adminv2api.UpdateShadowLinkResponse], error) {
	f.updateMasks = append(f.updateMasks, req.Msg.GetUpdateMask().GetPaths())
	return f.updateResults(len(f.updateMasks))
}

func okUpdateResponse() *connect.Response[adminv2api.UpdateShadowLinkResponse] {
	return connect.NewResponse(&adminv2api.UpdateShadowLinkResponse{
		ShadowLink: &adminv2api.ShadowLink{Status: &adminv2api.ShadowLinkStatus{}},
	})
}

func invalidUpdateMaskError() error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New("Invalid update mask: unknown path \"configurations.role_sync_options\""))
}

func TestSyncerLegacyUpdateMaskRetry(t *testing.T) {
	link := &redpandav1alpha2.ShadowLink{ObjectMeta: metav1.ObjectMeta{Name: "link"}}

	t.Run("retries with the legacy mask when the broker rejects the update mask", func(t *testing.T) {
		fake := &fakeShadowLinkService{
			updateResults: func(attempt int) (*connect.Response[adminv2api.UpdateShadowLinkResponse], error) {
				if attempt == 1 {
					// Broker that predates role sync rejects the full mask.
					return nil, invalidUpdateMaskError()
				}
				return okUpdateResponse(), nil
			},
		}
		syncer := &Syncer{shadowLinks: fake}

		_, err := syncer.Sync(context.Background(), link, RemoteClusterSettings{})
		require.NoError(t, err)

		require.Len(t, fake.updateMasks, 2, "expected an initial update and one retry")
		require.Equal(t, updateMaskPaths, fake.updateMasks[0], "first attempt should use the full mask")
		require.Equal(t, legacyUpdateMaskPaths, fake.updateMasks[1], "retry should use the legacy mask")
		require.NotContains(t, fake.updateMasks[1], "configurations.role_sync_options", "retry must drop the role sync path")
	})

	t.Run("does not retry when the update succeeds", func(t *testing.T) {
		fake := &fakeShadowLinkService{
			updateResults: func(attempt int) (*connect.Response[adminv2api.UpdateShadowLinkResponse], error) {
				return okUpdateResponse(), nil
			},
		}
		syncer := &Syncer{shadowLinks: fake}

		_, err := syncer.Sync(context.Background(), link, RemoteClusterSettings{})
		require.NoError(t, err)
		require.Len(t, fake.updateMasks, 1, "a successful update must not be retried")
		require.Equal(t, updateMaskPaths, fake.updateMasks[0])
	})

	t.Run("does not retry on unrelated errors", func(t *testing.T) {
		wantErr := connect.NewError(connect.CodeInternal, errors.New("boom"))
		fake := &fakeShadowLinkService{
			updateResults: func(attempt int) (*connect.Response[adminv2api.UpdateShadowLinkResponse], error) {
				return nil, wantErr
			},
		}
		syncer := &Syncer{shadowLinks: fake}

		_, err := syncer.Sync(context.Background(), link, RemoteClusterSettings{})
		require.Error(t, err)
		require.Len(t, fake.updateMasks, 1, "a non-mask error must not trigger the legacy retry")
	})
}

func TestIsInvalidUpdateMaskError(t *testing.T) {
	t.Run("nil is not a mask error", func(t *testing.T) {
		require.False(t, isInvalidUpdateMaskError(nil))
	})

	t.Run("invalid-argument with the mask message matches", func(t *testing.T) {
		require.True(t, isInvalidUpdateMaskError(invalidUpdateMaskError()))
	})

	t.Run("invalid-argument with a different message does not match", func(t *testing.T) {
		err := connect.NewError(connect.CodeInvalidArgument, errors.New("some other validation failure"))
		require.False(t, isInvalidUpdateMaskError(err))
	})

	t.Run("mask message under a different code does not match", func(t *testing.T) {
		err := connect.NewError(connect.CodeInternal, errors.New("Invalid update mask"))
		require.False(t, isInvalidUpdateMaskError(err))
	})

	t.Run("a plain (non-connect) error does not match", func(t *testing.T) {
		require.False(t, isInvalidUpdateMaskError(errors.New("Invalid update mask")))
	})
}

// TestLegacyUpdateMaskPaths locks the relationship between the full and legacy
// masks: the legacy mask must be exactly the full mask minus the role sync
// path, which is what makes old brokers accept the retried update.
func TestLegacyUpdateMaskPaths(t *testing.T) {
	require.Equal(t, updateMaskPaths[:len(updateMaskPaths)-1], legacyUpdateMaskPaths)
	require.Contains(t, updateMaskPaths, "configurations.role_sync_options")
	require.NotContains(t, legacyUpdateMaskPaths, "configurations.role_sync_options")
}
