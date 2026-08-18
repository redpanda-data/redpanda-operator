// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	corev1 "k8s.io/api/core/v1"
)

// ExportRedactSecretForTest exposes the package-private redactSecret to the
// external _test package. Test-only.
func ExportRedactSecretForTest(s *corev1.Secret, includePrivateKeys bool) *corev1.Secret {
	return redactSecret(s, includePrivateKeys)
}
