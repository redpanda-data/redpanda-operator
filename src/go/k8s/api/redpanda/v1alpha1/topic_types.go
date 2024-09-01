// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha1

import "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"

// Topic defines the CRD for Topic resources. See https://docs.redpanda.com/current/manage/kubernetes/manage-topics/.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Topic v1alpha2.Topic

// TopicList contains a list of Topic objects.
// +kubebuilder:object:root=true
type TopicList v1alpha2.TopicList
