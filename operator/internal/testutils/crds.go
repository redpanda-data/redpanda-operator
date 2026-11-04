// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package testutils

import (
	"context"
	"testing"
	"time"

	"github.com/redpanda-data/common-go/kube"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// InstallCRDs applies the given CRDs and waits for each to be Established.
func InstallCRDs(t *testing.T, ctl *kube.Ctl, crds ...*apiextensionsv1.CustomResourceDefinition) {
	t.Helper()

	// Apply writes the server's uid and resourceVersion into the object it is
	// given, and crds.ByName hands out package-level objects: applied as-is a
	// second time, to the next test's API server, they fail with a uid
	// mismatch.
	copies := make([]*apiextensionsv1.CustomResourceDefinition, 0, len(crds))
	for _, crd := range crds {
		copies = append(copies, crd.DeepCopy())
	}
	require.NoError(t, kube.ApplyAll(t.Context(), ctl, copies...))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, crd := range crds {
		require.NoError(t, kube.WaitFor(ctx, ctl, crd.DeepCopy(), func(crd *apiextensionsv1.CustomResourceDefinition, err error) (bool, error) {
			if err != nil {
				return false, err
			}

			for _, cond := range crd.Status.Conditions {
				if cond.Type == apiextensionsv1.Established {
					return cond.Status == apiextensionsv1.ConditionTrue, nil
				}
			}

			return false, nil
		}))
	}
}
