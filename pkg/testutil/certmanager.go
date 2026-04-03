// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutil

import (
	"context"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForCertManagerWebhook polls until the cert-manager-webhook service has
// at least one ready endpoint. This prevents flaky test failures caused by
// helm installs that create cert-manager Certificate resources before the
// webhook is ready to serve validation requests.
func WaitForCertManagerWebhook(ctx context.Context, c client.Client, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		var slices discoveryv1.EndpointSliceList
		if err := c.List(ctx, &slices,
			client.InNamespace("cert-manager"),
			client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labels.Set{
				discoveryv1.LabelServiceName: "cert-manager-webhook",
			})},
		); err != nil {
			return false, nil //nolint:nilerr // keep polling
		}
		for _, slice := range slices.Items {
			for _, ep := range slice.Endpoints {
				if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
					return true, nil
				}
			}
		}
		return false, nil
	})
}
