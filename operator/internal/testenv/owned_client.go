// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testenv

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newOwnedClient(c client.Client, owner metav1.OwnerReference) client.Client {
	return &ownedClient{
		Client: c,
		owner:  owner,
	}
}

var _ client.Client = &ownedClient{}

// ownedClient is a Client that wraps another Client in order to enforce
// ownership to at least the provided reference.
type ownedClient struct {
	client.Client
	owner metav1.OwnerReference
}

// Create implements client.Client.
func (n *ownedClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	n.maybeAddOwnerRef(obj)
	return n.Client.Create(ctx, obj, opts...)
}

// Update implements client.Client.
func (n *ownedClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	n.maybeAddOwnerRef(obj)
	return n.Client.Update(ctx, obj, opts...)
}

// Patch implements client.Client.
func (n *ownedClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	n.maybeAddOwnerRef(obj)
	return n.Client.Patch(ctx, obj, patch, opts...)
}

func (c *ownedClient) maybeAddOwnerRef(obj client.Object) {
	// We don't care about namespace objects as deleting the namespace will GC
	// them.
	if ok, _ := c.IsObjectNamespaced(obj); ok {
		return
	}

	// If this obj already has an owner, rely on that owner to GC it.
	if len(obj.GetOwnerReferences()) > 0 {
		return
	}

	// Otherwise inject a reference to our owner.
	obj.SetOwnerReferences([]metav1.OwnerReference{*c.owner.DeepCopy()})
}
