// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"slices"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/kube"
	"k8s.io/utils/ptr"

	redpandav25 "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/multicluster"
)

// ConvertStretchClusterToRenderState converts a Stretch Cluster CRD to a redpanda chart RenderState.
func ConvertStretchClusterToRenderState(config *kube.RESTConfig, defaulters *V2Defaulters, cluster *redpandav1alpha2.StretchCluster, pools []*redpandav1alpha2.NodePool, clusterName string) (*redpanda.RenderState, error) {
	rp := &redpandav1alpha2.Redpanda{
		ObjectMeta: cluster.ObjectMeta,
		Spec: redpandav1alpha2.RedpandaSpec{
			ClusterSpec: &redpandav1alpha2.RedpandaClusterSpec{
				Console: &redpandav1alpha2.RedpandaConsole{
					Enabled: ptr.To(false),
				},
				Statefulset: &redpandav1alpha2.Statefulset{
					Replicas: ptr.To(0),
				},
			},
		},
	}

	rp.Name = rp.ObjectMeta.Name + "-" + clusterName

	if err := convertJSONNotNil(&cluster.Spec, rp.Spec.ClusterSpec); err != nil {
		return nil, errors.WithStack(err)
	}

	spec := defaultV2Spec(defaulters, rp)

	dot, err := redpanda.Chart.Dot(config, helmette.Release{
		Namespace: rp.Namespace,
		Name:      rp.GetHelmReleaseName(),
		Service:   "Helm",
		IsUpgrade: true,
	}, spec)
	if err != nil {
		return nil, err
	}

	return redpanda.RenderStateFromDot(dot, func(state *redpanda.RenderState) error {
		v25State := &redpandav25.RenderState{
			Release:               state.Release,
			Files:                 state.Files,
			Chart:                 state.Chart,
			Values:                redpandav25.Values{},
			BootstrapUserSecret:   state.BootstrapUserSecret,
			BootstrapUserPassword: state.BootstrapUserPassword,
			StatefulSetPodLabels:  state.StatefulSetPodLabels,
			StatefulSetSelector:   state.StatefulSetSelector,
			Dot:                   state.Dot,
		}
		if err := convertV2Fields(v25State, &v25State.Values, spec); err != nil {
			return err
		}

		renderedPools, err := convertV2NodepoolsToPools(v25State.Values, pools, defaulters)
		if err != nil {
			return err
		}

		slices.SortStableFunc(renderedPools, func(poolA, poolB redpandav25.Pool) int {
			return strings.Compare(poolA.Name, poolB.Name)
		})

		return convertAndAppendJSONNotNil(renderedPools, &state.Pools)
	})
}
