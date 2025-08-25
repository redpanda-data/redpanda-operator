// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

func RenderResourcesFromCRD(cfg *kube.RESTConfig, release helmette.Release, cluster any, pools []*redpandav1alpha3.NodePool) (_ []kube.Object, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	dot, err := Chart.Dot(cfg, release, cluster)
	if err != nil {
		return nil, err
	}

	return renderResources(dot, pools), nil
}

func RenderNodePoolsFromCRD(cfg *kube.RESTConfig, release helmette.Release, cluster any, pools []*redpandav1alpha3.NodePool) (_ []*appsv1.StatefulSet, err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "chart execution failed")
		default:
			err = errors.Newf("chart execution failed: %#v", r)
		}
	}()

	dot, err := Chart.Dot(cfg, release, cluster)
	if err != nil {
		return nil, err
	}

	checkVersion(dot)

	return StatefulSets(dot, pools), nil
}

func checkVersion(dot *helmette.Dot) {
	values := helmette.Unwrap[Values](dot.Values)

	if !RedpandaAtLeast_22_2_0(dot) && !values.Force {
		sv := semver(dot)
		panic(fmt.Sprintf("Error: The Redpanda version (%s) is no longer supported \nTo accept this risk, run the upgrade again adding `--force=true`\n", sv))
	}
}
