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

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// InstallCRDs applies the given CRDs and waits for each to be Established.
func InstallCRDs(t *testing.T, ctl *kube.Ctl, crds ...*apiextensionsv1.CustomResourceDefinition) {
	t.Helper()

	require.NoError(t, kube.ApplyAll(t.Context(), ctl, crds...))

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
