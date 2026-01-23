// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package kube

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestItems(t *testing.T) {
	// Unlike other types, ServiceMonitorList's Items is []*T instead of []T.
	// As we need to use reflection to get Items out, we need to be careful to
	// not panic.
	_, err := Items[Object](&monitoringv1.ServiceMonitorList{Items: []*monitoringv1.ServiceMonitor{{}}})
	require.NoError(t, err)

	_, err = Items[*monitoringv1.ServiceMonitor](&monitoringv1.ServiceMonitorList{Items: []*monitoringv1.ServiceMonitor{{}}})
	require.NoError(t, err)

	_, err = Items[Object](&corev1.PodList{Items: []corev1.Pod{{}}})
	require.NoError(t, err)

	_, err = Items[*corev1.Pod](&corev1.PodList{Items: []corev1.Pod{{}}})
	require.NoError(t, err)

	_, err = Items[Object](&BogusList{Items: []struct{}{{}}})
	require.EqualError(t, err, "can't convert struct {} to client.Object")
}

type BogusList struct {
	Items []struct{}
}

var _ client.ObjectList = (*BogusList)(nil)

func (*BogusList) GetObjectKind() schema.ObjectKind  { return schema.EmptyObjectKind }
func (*BogusList) DeepCopyObject() runtime.Object    { return nil }
func (*BogusList) GetResourceVersion() string        { return "" }
func (*BogusList) SetResourceVersion(version string) {}
func (*BogusList) GetSelfLink() string               { return "" }
func (*BogusList) SetSelfLink(selfLink string)       {}
func (*BogusList) GetContinue() string               { return "" }
func (*BogusList) SetContinue(c string)              {}
func (*BogusList) GetRemainingItemCount() *int64     { return nil }
func (*BogusList) SetRemainingItemCount(c *int64)    {}
