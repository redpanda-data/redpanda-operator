// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package rackawareness is an intentionally empty package that houses the
// kubebuilder annotations for generating the ClusterRole required for the
// redpanda helm chart's rack-awareness feature. As the operator will need the
// same ClusterRole in order to create the ClusterRole for its deployments, it
// is housed in the operator.
package rackawareness

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get
