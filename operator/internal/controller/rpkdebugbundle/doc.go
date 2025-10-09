// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package rpkdebugbundle is an intentionally empty package that houses the
// kubebuilder annotations for generating the Roles required to run `rpk debug
// bundle`[1].
//
// [1]: https://github.com/redpanda-data/redpanda/blob/93edacb1d4c802c47d239cf7bbdc1660c869bd01/src/go/rpk/pkg/cli/debug/bundle/bundle_k8s_linux.go#L492-L501
package rpkdebugbundle

// +kubebuilder:rbac:groups="",namespace=default,resources=configmaps;endpoints;events;limitranges;persistentvolumeclaims;pods;pods/log;replicationcontrollers;resourcequotas;serviceaccounts;services,verbs=get;list
