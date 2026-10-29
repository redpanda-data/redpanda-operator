// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package pipelinesrbac holds the RBAC the Console controller needs for
// console.PipelinesTypes. It's a bundle of its own so that the operator chart
// grants it only with connectController.enabled.
package pipelinesrbac

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers;certificates,verbs=get;list;watch;create;update;patch;delete
// RBAC escalation checks require holding every permission the pipeline API's
// Role grants. Binding system:auth-delegator needs the TokenReview and
// SubjectAccessReview creates that the leader-election bundle already grants.
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines,verbs=get;list;create;update;delete
