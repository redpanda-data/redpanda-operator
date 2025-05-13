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

	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2NodePoolRenderer represents a node pool renderer for v2 clusters.
type V2NodePoolRenderer struct {
	kubeConfig   *kube.RESTConfig
	image        Image
	cloudSecrets CloudSecretsFlags
}

var _ NodePoolRenderer[redpandav1alpha2.Redpanda, *redpandav1alpha2.Redpanda] = (*V2NodePoolRenderer)(nil)

// NewV2NodePoolRenderer returns a V2NodePoolRenderer.
func NewV2NodePoolRenderer(mgr ctrl.Manager, image Image, cloudSecrets CloudSecretsFlags) *V2NodePoolRenderer {
	return &V2NodePoolRenderer{
		kubeConfig:   mgr.GetConfig(),
		image:        image,
		cloudSecrets: cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V2NodePoolRenderer) Render(ctx context.Context, cluster *redpandav1alpha2.Redpanda) ([]*appsv1.StatefulSet, error) {
	spec := cluster.Spec.ClusterSpec.DeepCopy()
	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	if spec.Statefulset == nil {
		spec.Statefulset = &redpandav1alpha2.Statefulset{}
	}

	if spec.Statefulset.InitContainerImage == nil {
		spec.Statefulset.InitContainerImage = &redpandav1alpha2.InitContainerImage{}
	}

	if spec.Statefulset.SideCars == nil {
		spec.Statefulset.SideCars = &redpandav1alpha2.SideCars{}
	}

	if spec.Statefulset.SideCars.Image == nil {
		spec.Statefulset.SideCars.Image = &redpandav1alpha2.RedpandaImage{}
	}

	// If not explicitly specified, set the tag and repository of the sidecar
	// to the image specified via CLI args rather than relying on the default
	// of the redpanda chart.
	// This ensures that custom deployments (e.g.
	// localhost/redpanda-operator:dev) will use the image they are deployed
	// with.
	if spec.Statefulset.SideCars.Image.Tag == nil {
		spec.Statefulset.SideCars.Image.Tag = &m.image.Tag
	}

	if spec.Statefulset.SideCars.Image.Repository == nil {
		spec.Statefulset.SideCars.Image.Repository = &m.image.Repository
	}

	if spec.Statefulset.InitContainerImage.Tag == nil {
		spec.Statefulset.InitContainerImage.Tag = &m.image.Tag
	}

	if spec.Statefulset.InitContainerImage.Repository == nil {
		spec.Statefulset.InitContainerImage.Repository = &m.image.Repository
	}

	// If not explicitly specified, set the initContainer flags for the bootstrap
	// templating to instantiate an appropriate CloudExpander
	if spec.Statefulset.InitContainers == nil {
		spec.Statefulset.InitContainers = &redpandav1alpha2.InitContainers{}
	}

	if spec.Statefulset.InitContainers.Configurator == nil {
		spec.Statefulset.InitContainers.Configurator = &redpandav1alpha2.Configurator{}
	}

	if len(spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs) == 0 {
		spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs = m.cloudSecrets.AdditionalConfiguratorArgs()
	}

	rendered, err := redpanda.Chart.Render(m.kubeConfig, helmette.Release{
		Namespace: cluster.Namespace,
		Name:      cluster.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, spec)
	if err != nil {
		return nil, err
	}

	resources := []*appsv1.StatefulSet{}

	// filter out non-nodepools
	for _, object := range rendered {
		if isNodePool(object) {
			resources = append(resources, object.(*appsv1.StatefulSet))
		}
	}

	return resources, nil
}

// isNodePool returns whether or not the object passed to it should be considered a node pool.
// For now, this concrete implementation just looks for any StatefulSets and says that they are a
// node pool.
func isNodePool(object client.Object) bool {
	return isStatefulSet(object)
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *V2NodePoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}
