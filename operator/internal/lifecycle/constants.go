// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import "sigs.k8s.io/controller-runtime/pkg/client"

const (
	defaultFieldOwner     = client.FieldOwner("cluster.redpanda.com/operator")
	defaultNamespaceLabel = "cluster.redpanda.com/namespace"
	defaultOperatorLabel  = "cluster.redpanda.com/operator"
	defaultOwnerLabel     = "cluster.redpanda.com/owner"
	generationLabel       = "cluster.redpanda.com/generation"
	componentLabel        = "app.kubernetes.io/component"
	instanceLabel         = "app.kubernetes.io/instance"
	fluxNameLabel         = "helm.toolkit.fluxcd.io/name"
	fluxNamespaceLabel    = "helm.toolkit.fluxcd.io/namespace"
)
