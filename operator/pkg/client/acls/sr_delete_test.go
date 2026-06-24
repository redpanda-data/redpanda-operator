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
	"testing"

	"github.com/redpanda-data/common-go/rpsr"
	"github.com/stretchr/testify/require"
)

// fakeSRACLClient is a test double for rpsr.ACLClient. It records DeleteACLs
// invocations and lets a test inject the failure a licenseless broker returns
// for DELETE /security/acls.
type fakeSRACLClient struct {
	listResult   []rpsr.ACL
	listErr      error
	deleteErr    error
	deleteCalled bool
	deletedACLs  []rpsr.ACL
}

func (f *fakeSRACLClient) ListACLs(_ context.Context, _ *rpsr.ACL) ([]rpsr.ACL, error) {
	return f.listResult, f.listErr
}

func (f *fakeSRACLClient) ListACLsBatch(_ context.Context, _ []rpsr.ACL) ([]rpsr.ACL, error) {
	return f.listResult, f.listErr
}

func (f *fakeSRACLClient) CreateACLs(_ context.Context, _ []rpsr.ACL) error {
	return nil
}

func (f *fakeSRACLClient) DeleteACLs(_ context.Context, acls []rpsr.ACL) error {
	f.deleteCalled = true
	f.deletedACLs = acls
	return f.deleteErr
}

// TestDeleteAllSRACLSkipsEmptyDelete is a regression test for #1623. On a
// cluster without an enterprise license, listing SR ACLs is permitted and
// returns nothing, but issuing a DeleteACLs request (DELETE /security/acls) —
// even with an empty list — is rejected with "Invalid license: not present".
// That error blocked the User finalizer, leaving the object stuck Terminating.
// deleteAllSRACL must therefore skip the delete when there is nothing to
// delete, so the enterprise gate can never block user deletion.
func TestDeleteAllSRACLSkipsEmptyDelete(t *testing.T) {
	fake := &fakeSRACLClient{
		listResult: nil, // a licenseless cluster has no SR ACLs to begin with
		deleteErr:  errors.New("Invalid license: not present"),
	}
	s := &Syncer{srClient: fake}

	err := s.deleteAllSRACL(context.Background(), "User:alice")
	require.NoError(t, err)
	require.False(t, fake.deleteCalled,
		"DeleteACLs must not be called when there are no SR ACLs to delete")
}

// TestDeleteAllSRACLDeletesWhenPresent documents that the guard only skips the
// empty case: when SR ACLs exist they are still deleted.
func TestDeleteAllSRACLDeletesWhenPresent(t *testing.T) {
	fake := &fakeSRACLClient{
		listResult: []rpsr.ACL{{Principal: "User:alice"}},
	}
	s := &Syncer{srClient: fake}

	err := s.deleteAllSRACL(context.Background(), "User:alice")
	require.NoError(t, err)
	require.True(t, fake.deleteCalled, "DeleteACLs must be called when SR ACLs exist")
	require.Len(t, fake.deletedACLs, 1)
}
