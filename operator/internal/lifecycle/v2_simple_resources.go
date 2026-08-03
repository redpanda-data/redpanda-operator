// Copyright 2026 Redpanda Data, Inc.
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
	"fmt"

	"github.com/redpanda-data/common-go/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2/conversion"
)

// V2SimpleResourceRenderer represents an simple resource renderer for v2 clusters.
type V2SimpleResourceRenderer struct {
	kubeConfig *kube.RESTConfig
}

var _ SimpleResourceRenderer[ClusterWithPools, *ClusterWithPools] = (*V2SimpleResourceRenderer)(nil)

// NewV2SimpleResourceRenderer returns a V2SimpleResourceRenderer.
func NewV2SimpleResourceRenderer(mgr ctrl.Manager) *V2SimpleResourceRenderer {
	return &V2SimpleResourceRenderer{
		kubeConfig: mgr.GetConfig(),
	}
}

// Render returns a list of simple resources for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// should be considered a node pool.
func (m *V2SimpleResourceRenderer) Render(ctx context.Context, cluster *ClusterWithPools, _ string) ([]client.Object, error) {
	spec := cluster.Spec.ClusterSpec.DeepCopy()

	if spec != nil {
		// normalize the spec by removing the connectors stanza since it's deprecated
		spec.Connectors = nil
	}

	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	// NB: No need for real defaults here as the defaults are leveraged only in the stateful set
	// images and pod command-line args, neither of which are looked at for rendering simple
	// resources.
	state, err := conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaulters{}, cluster.Redpanda, cluster.NodePools)
	if err != nil {
		return nil, err
	}

	// disable the console spec components so we don't try to render it twice
	state.Values.Console.Enabled = ptr.To(false)

	resources, err := redpanda.RenderResources(state)
	if err != nil {
		return nil, err
	}

	console, err := m.consoleIntegration(cluster, spec.Console)
	if err != nil {
		return nil, err
	}

	if console != nil {
		resources = append(resources, console)
	}

	return resources, err
}

func (m *V2SimpleResourceRenderer) consoleIntegration(
	cluster *ClusterWithPools,
	console *redpandav1alpha2.RedpandaConsole,
) (*redpandav1alpha2.Console, error) {
	values, err := redpandav1alpha2.ConvertConsoleSubchartToConsoleValues(console)
	if err != nil {
		return nil, err
	}

	// Values can be nil if console is disabled.
	if values == nil {
		return nil, nil
	}

	return &redpandav1alpha2.Console{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Console",
			APIVersion: redpandav1alpha2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: redpandav1alpha2.ConsoleSpec{
			ConsoleValues: *values,
			ClusterSource: &redpandav1alpha2.ClusterSource{
				ClusterRef: &redpandav1alpha2.ClusterRef{
					Name: cluster.Name,
				},
			},
		},
	}, nil
}

// WatchedResourceTypes returns the list of all the resources that the cluster
// controller needs to watch.
func (m *V2SimpleResourceRenderer) WatchedResourceTypes() []client.Object {
	return redpanda.Types()
}

// MigratingResources returns a list of resources that need to be migrated
// away from being managed by the Redpanda CRD.
func (m *V2SimpleResourceRenderer) MigratingResources() []client.Object {
	return []client.Object{
		&redpandav1alpha2.Console{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Console",
				APIVersion: redpandav1alpha2.GroupVersion.String(),
			},
		},
	}
}

// GetAdminAPIEndpoints returns the admin-API endpoint ("<pod-fqdn>:<port>") for
// every desired pod across the cluster's pools. The first DNS label of each is
// the pod name, which podAdminEndpoint-style lookups rely on to dial a specific
// broker (stale-disk wipe, ghost maintenance-mode clears). Like Render it needs
// no defaulters (endpoints depend only on names, replicas, cluster domain, and
// admin port); a conversion failure returns nil and callers treat a missing
// endpoint as "cannot verify, do nothing".
func (m *V2SimpleResourceRenderer) GetAdminAPIEndpoints(cluster *ClusterWithPools) []string {
	state, err := conversion.ConvertV2ToRenderState(m.kubeConfig, &conversion.V2Defaulters{}, cluster.Redpanda, cluster.NodePools)
	if err != nil {
		return nil
	}

	// The default statefulset is not among state.Pools (those hold only
	// NodePool-derived pools); mirror the chart's StatefulSets renderer, which
	// prepends it as an unnamed Pool.
	pools := append([]redpanda.Pool{{Statefulset: state.Values.Statefulset}}, state.Pools...)

	var endpoints []string
	for _, pool := range pools {
		poolFullname := redpanda.Fullname(state) + pool.Suffix()
		for i := int32(0); i < pool.Statefulset.Replicas; i++ {
			endpoints = append(endpoints, fmt.Sprintf("%s-%d.%s:%d", poolFullname, i, redpanda.InternalDomain(state), state.Values.Listeners.Admin.Port))
		}
	}
	return endpoints
}
