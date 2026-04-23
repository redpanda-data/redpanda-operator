// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

type testRemoteClusterObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	source       *redpandav1alpha2.ClusterSource
	remoteSource *redpandav1alpha2.ClusterSource
}

func (t *testRemoteClusterObject) DeepCopyObject() runtime.Object {
	return t.DeepCopy()
}

func (t *testRemoteClusterObject) DeepCopy() *testRemoteClusterObject {
	out := *t
	if t.source != nil {
		out.source = t.source.DeepCopy()
	}
	if t.remoteSource != nil {
		out.remoteSource = t.remoteSource.DeepCopy()
	}
	return &out
}

func (t *testRemoteClusterObject) GetClusterSource() *redpandav1alpha2.ClusterSource {
	return t.source
}

func (t *testRemoteClusterObject) GetRemoteClusterSource() *redpandav1alpha2.ClusterSource {
	return t.remoteSource
}

func TestIndexByClusterSourceIncludesRemoteClusterNamespace(t *testing.T) {
	index := indexByClusterSource(func(cr *redpandav1alpha2.ClusterRef) bool {
		return cr.IsV2()
	})

	obj := &testRemoteClusterObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shadow-link",
			Namespace: "console",
		},
		remoteSource: &redpandav1alpha2.ClusterSource{
			ClusterRef: &redpandav1alpha2.ClusterRef{
				Name:      "remote-redpanda",
				Namespace: ptr.To("remote-ns"),
			},
		},
	}

	require.Equal(t, []string{
		types.NamespacedName{Namespace: "remote-ns", Name: "remote-redpanda"}.String(),
	}, index(obj))
}

func TestIndexByClusterSourceIncludesLocalAndRemoteClusters(t *testing.T) {
	index := indexByClusterSource(func(cr *redpandav1alpha2.ClusterRef) bool {
		return cr.IsV2()
	})

	obj := &testRemoteClusterObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shadow-link",
			Namespace: "console",
		},
		source: &redpandav1alpha2.ClusterSource{
			ClusterRef: &redpandav1alpha2.ClusterRef{
				Name: "local-redpanda",
			},
		},
		remoteSource: &redpandav1alpha2.ClusterSource{
			ClusterRef: &redpandav1alpha2.ClusterRef{
				Name:      "remote-redpanda",
				Namespace: ptr.To("remote-ns"),
			},
		},
	}

	require.Equal(t, []string{
		types.NamespacedName{Namespace: "console", Name: "local-redpanda"}.String(),
		types.NamespacedName{Namespace: "remote-ns", Name: "remote-redpanda"}.String(),
	}, index(obj))
}
