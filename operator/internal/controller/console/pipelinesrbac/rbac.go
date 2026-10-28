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

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingadmissionpolicies;validatingadmissionpolicybindings,verbs=get;list;watch;create;update;patch;delete
// RBAC escalation checks require holding every permission the pipelines Role and ClusterRoles grant.
// +kubebuilder:rbac:groups=cluster.redpanda.com,resources=pipelines,verbs=get;list;watch;create;update;patch;delete
