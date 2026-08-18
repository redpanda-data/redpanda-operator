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

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"

	entv1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/enterprise/operator/render"
	"github.com/redpanda-data/redpanda-operator/enterprise/pkg/multicluster"
)

// defaultBrokerImage mirrors defaultImage for the enterprise RedpandaImage
// type carried by RedpandaBrokerPool specs (field-for-field identical to the
// OSS RedpandaImage).
func defaultBrokerImage(default_ Image) func(*entv1alpha2.RedpandaImage) *entv1alpha2.RedpandaImage {
	return func(base *entv1alpha2.RedpandaImage) *entv1alpha2.RedpandaImage {
		if base == nil {
			return &entv1alpha2.RedpandaImage{
				Repository: ptr.To(default_.Repository),
				Tag:        ptr.To(default_.Tag),
			}
		}
		return &entv1alpha2.RedpandaImage{
			Repository: ptr.To(ptr.Deref(base.Repository, default_.Repository)),
			Tag:        ptr.To(ptr.Deref(base.Tag, default_.Tag)),
		}
	}
}

// NodePoolRenderer represents a node pool renderer for stretch clusters.
type StretchBrokerPoolRenderer struct {
	mgr           multicluster.Manager
	sideCarImage  Image
	redpandaImage Image
	cloudSecrets  CloudSecretsFlags
}

var _ NodePoolRenderer = (*StretchBrokerPoolRenderer)(nil)

// NewStretchBrokerPoolRenderer returns a StretchNodePoolRenderer.
func NewStretchBrokerPoolRenderer(mgr multicluster.Manager, redpandaImage, sideCarImage Image, cloudSecrets CloudSecretsFlags) *StretchBrokerPoolRenderer {
	return &StretchBrokerPoolRenderer{
		mgr:           mgr,
		sideCarImage:  sideCarImage,
		redpandaImage: redpandaImage,
		cloudSecrets:  cloudSecrets,
	}
}

// Render returns a list of StatefulSets for the given stretch cluster.
func (m *StretchBrokerPoolRenderer) Render(ctx context.Context, cluster *StretchClusterWithPools, clusterName string) ([]*appsv1.StatefulSet, error) {
	cl, err := m.mgr.GetCluster(ctx, clusterName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Use the canonical cluster name so that labels are identical regardless
	// of which operator instance (local vs remote) performs the reconciliation.
	canonicalName := CanonicalClusterName(clusterName, m.mgr.GetLocalClusterName)

	// Apply operator-level default images to pools that don't specify their own.
	applyDefaultImage := defaultBrokerImage(m.redpandaImage)
	applyDefaultSidecar := defaultBrokerImage(m.sideCarImage)
	inCluster := cluster.GetBrokerPoolsForCluster(canonicalName)
	for _, pool := range inCluster {
		pool.Spec.Image = applyDefaultImage(pool.Spec.Image)
		pool.Spec.SidecarImage = applyDefaultSidecar(pool.Spec.SidecarImage)
	}
	allPools := cluster.GetAllBrokerPools()
	for _, pool := range allPools {
		pool.Spec.Image = applyDefaultImage(pool.Spec.Image)
		pool.Spec.SidecarImage = applyDefaultSidecar(pool.Spec.SidecarImage)
	}

	state, err := render.NewRenderState(
		cl.GetConfig(),
		cluster.StretchCluster,
		inCluster,
		allPools,
		canonicalName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return render.RenderBrokerPools(state)
}

// IsNodePool returns whether or not the object passed to it should be considered a node pool.
func (m *StretchBrokerPoolRenderer) IsNodePool(object client.Object) bool {
	return isNodePool(object)
}

func CanonicalClusterName(clusterName string, getLocalClusterName func() string) string {
	canonicalName := clusterName
	if canonicalName == mcmanager.LocalCluster {
		canonicalName = getLocalClusterName()
	}
	return canonicalName
}
