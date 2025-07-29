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
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// V2NodePoolRenderer represents a node pool renderer for v2 clusters.
type V2NodePoolRenderer struct {
	kubeConfig    *kube.RESTConfig
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer[ClusterWithPools, *ClusterWithPools] = (*V2NodePoolRenderer)(nil)

// NewV2NodePoolRenderer returns a V2NodePoolRenderer.
func NewV2NodePoolRenderer(mgr ctrl.Manager, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *V2NodePoolRenderer {
	return &V2NodePoolRenderer{
		kubeConfig:    mgr.GetConfig(),
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given Redpanda v2 cluster. It does this by
// delegating to our particular resource rendering pipeline and filtering out anything that
// isn't a node pool.
func (m *V2NodePoolRenderer) Render(ctx context.Context, cluster *ClusterWithPools) ([]*appsv1.StatefulSet, error) {
	// Big ol' block of defaulting to avoid nil dereferences.
	spec := cluster.Spec.ClusterSpec.DeepCopy()
	if spec == nil {
		spec = &redpandav1alpha2.RedpandaClusterSpec{}
	}

	if spec.Statefulset == nil {
		spec.Statefulset = &redpandav1alpha2.Statefulset{}
	}

	if spec.Statefulset.SideCars == nil {
		spec.Statefulset.SideCars = &redpandav1alpha2.SideCars{}
	}

	if spec.Statefulset.SideCars.Controllers == nil {
		spec.Statefulset.SideCars.Controllers = &redpandav1alpha2.RPControllers{}
	}

	if spec.Statefulset.InitContainers == nil {
		spec.Statefulset.InitContainers = &redpandav1alpha2.InitContainers{}
	}

	if spec.Statefulset.InitContainers.Configurator == nil {
		spec.Statefulset.InitContainers.Configurator = &redpandav1alpha2.Configurator{}
	}

	// As we're currently pinned to the v5.10.x chart, we need to set the
	// default image to the operator's preferred version.
	spec.Image = defaultImage(spec.Image, m.redpandaImage)

	// The flag that disables cluster configuration synchronization is set to `true` to not
	// conflict with operator cluster configuration synchronization.
	spec.Statefulset.SideCars.Args = []string{"--no-set-superusers"}

	// If not explicitly specified, set the tag and repository of the sidecar
	// to the image specified via CLI args rather than relying on the default
	// of the redpanda chart.
	// This ensures that custom deployments (e.g.
	// localhost/redpanda-operator:dev) will use the image they are deployed
	// with.
	spec.Statefulset.SideCars.Image = defaultImage(spec.Statefulset.SideCars.Image, m.sideCarImage)
	spec.Statefulset.SideCars.Controllers.Image = defaultImage(spec.Statefulset.SideCars.Image, m.sideCarImage)

	// If not explicitly specified, set the initContainer flags for the bootstrap
	// templating to instantiate an appropriate CloudExpander
	if len(spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs) == 0 {
		spec.Statefulset.InitContainers.Configurator.AdditionalCLIArgs = m.cloudSecrets.AdditionalConfiguratorArgs()
	}

	// TODO: upgrade the chart to redpanda/v25 by performing a conversion of
	// v1alpha2 to it's values here.
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

func defaultImage(base *redpandav1alpha2.RedpandaImage, default_ Image) *redpandav1alpha2.RedpandaImage {
	if base == nil {
		return &redpandav1alpha2.RedpandaImage{
			Repository: ptr.To(default_.Repository),
			Tag:        ptr.To(default_.Tag),
		}
	}

	return &redpandav1alpha2.RedpandaImage{
		Repository: ptr.To(ptr.Deref(base.Repository, default_.Repository)),
		Tag:        ptr.To(ptr.Deref(base.Tag, default_.Tag)),
	}
}
