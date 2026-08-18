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
	entmulticluster "github.com/redpanda-data/redpanda-operator/enterprise/pkg/multicluster"
	"github.com/redpanda-data/redpanda-operator/pkg/multicluster"
)

// The enterprise module's multicluster.Manager is a structural mirror of the
// OSS pkg/multicluster.Manager: neither module imports the other, so the raft
// manager built enterprise-side satisfies the OSS interface (and the OSS
// single-cluster/static managers satisfy the enterprise one) purely by method
// set. These assertions pin the two interfaces together — editing one without
// the other breaks this build.
func _(m entmulticluster.Manager) multicluster.Manager { return m }

func _(m multicluster.Manager) entmulticluster.Manager { return m }
