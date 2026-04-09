// Copyright 2026 Redpanda Data, Inc.
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
	DefaultFieldOwner     = client.FieldOwner("cluster.redpanda.com/operator")
	DefaultNamespaceLabel = "cluster.redpanda.com/namespace"
	defaultOperatorLabel  = "cluster.redpanda.com/operator"
	defaultOwnerLabel     = "cluster.redpanda.com/owner"
	generationLabel       = "cluster.redpanda.com/generation"
	configVersionLabel    = "cluster.redpanda.com/configVersion"
	// GCLabel is applied to out-of-band resources (Endpoints, EndpointSlices)
	// that should retain ownership labels for tracking but should not be
	// garbage collected by the syncer.
	GCLabel            = "cluster.redpanda.com/gc"
	componentLabel     = "app.kubernetes.io/component"
	instanceLabel      = "app.kubernetes.io/instance"
	fluxNameLabel      = "helm.toolkit.fluxcd.io/name"
	fluxNamespaceLabel = "helm.toolkit.fluxcd.io/namespace"
)
