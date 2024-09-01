// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/vectorized/v1alpha1"
)

func TestFinalizerTimeout(t *testing.T) {
	tcs := []struct {
		annotation *string
		deletion   time.Time
		expired    bool
		error      bool
	}{
		{
			annotation: nil,
			expired:    false,
		},
		{
			annotation: asRef(""),
			expired:    false,
		},
		{
			annotation: asRef("5m"),
			expired:    false,
		},
		{
			annotation: asRef("10m"),
			deletion:   time.Now().Add(-6 * time.Minute),
			expired:    false,
		},
		{
			annotation: asRef("5m"),
			deletion:   time.Now().Add(-6 * time.Minute),
			expired:    true,
		},
		{
			annotation: asRef("5xm"),
			error:      true,
		},
	}
	for idx, tc := range tcs {
		t.Run(fmt.Sprintf("test-%d", idx), func(t *testing.T) {
			c := vectorizedv1alpha1.Console{}
			if tc.annotation != nil {
				c.ObjectMeta.Annotations = map[string]string{
					vectorizedv1alpha1.FinalizersTimeoutAnnotation: *tc.annotation,
				}
			}
			if !tc.deletion.IsZero() {
				c.DeletionTimestamp = &metav1.Time{Time: tc.deletion}
			}
			ex, err := vectorizedv1alpha1.FinalizersExpired(&c)
			if tc.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expired, ex)
			}
		})
	}
}

func asRef(s string) *string {
	return &s
}
