// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package lifecycle

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2SimpleResourceRenderer represents an simple resource renderer for v2 clusters.
type StretchSimpleResourceRenderer struct {
	kubeConfig *kube.RESTConfig
}

var _ SimpleResourceRenderer[StretchClusterWithPools, *StretchClusterWithPools] = (*StretchSimpleResourceRenderer)(nil)

// NewStretchSimpleResourceRenderer returns a StretchSimpleResourceRenderer.
func NewStretchSimpleResourceRenderer(mgr cluster.Cluster) *StretchSimpleResourceRenderer {
	return &StretchSimpleResourceRenderer{
		kubeConfig: mgr.GetConfig(),
	}
}

// Render returns a list of simple resources for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// should be considered a node pool.
func (m *StretchSimpleResourceRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]client.Object, error) {
	//spec := cluster.Spec.ClusterSpec.DeepCopy()
	//
	//if spec != nil {
	//	// normalize the spec by removing the connectors stanza since it's deprecated
	//	spec.Connectors = nil
	//}
	//
	//if spec == nil {
	//	spec = &redpandav1alpha2.RedpandaClusterSpec{}
	//}

	// NB: No need for real defaults here as the defaults are leveraged only in the stateful set
	// images and pod command-line args, neither of which are looked at for rendering simple
	// resources.
	//state, err := conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaulters{}, cluster.Redpanda, cluster.NodePools)
	//if err != nil {
	//	return nil, err
	//}
	//
	//// disable the console spec components so we don't try to render it twice
	//state.Values.Console.Enabled = ptr.To(false)

	state := &redpanda.RenderState{}
	resources, err := redpanda.RenderResources(state)
	if err != nil {
		return nil, err
	}

	//console, err := m.consoleIntegration(cluster, spec.Console)
	//if err != nil {
	//	return nil, err
	//}
	//
	//if console != nil {
	//	resources = append(resources, console)
	//}

	return resources, err
}

//func (m *V2SimpleResourceRenderer) consoleIntegration(
//	cluster *ClusterWithPools,
//	console *redpandav1alpha2.RedpandaConsole,
//) (*redpandav1alpha2.Console, error) {
//	values, err := redpandav1alpha2.ConvertConsoleSubchartToConsoleValues(console)
//	if err != nil {
//		return nil, err
//	}
//
//	// Values can be nil if console is disabled.
//	if values == nil {
//		return nil, nil
//	}
//
//	return &redpandav1alpha2.Console{
//		TypeMeta: metav1.TypeMeta{
//			Kind:       "Console",
//			APIVersion: redpandav1alpha2.GroupVersion.String(),
//		},
//		ObjectMeta: metav1.ObjectMeta{
//			Name:      cluster.Name,
//			Namespace: cluster.Namespace,
//		},
//		Spec: redpandav1alpha2.ConsoleSpec{
//			ConsoleValues: *values,
//			ClusterSource: &redpandav1alpha2.ClusterSource{
//				ClusterRef: &redpandav1alpha2.ClusterRef{
//					Name: cluster.Name,
//				},
//			},
//		},
//	}, nil
//}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *StretchSimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return redpanda.Types()
}
