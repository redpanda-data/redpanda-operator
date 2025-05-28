// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_render.go.tpl
package redpanda

import (
	"fmt"

	"github.com/cockroachdb/errors"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/redpanda-data/redpanda-operator/charts/console/v3"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha3 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha3"
	"github.com/redpanda-data/redpanda-operator/pkg/kube"
)

// This serves as a running list of places in the v2 chart rendering functions that will need an update to be NodePool-aware:
// - configmap.tpl.go: RedpandaConfigFile (node configuration), RPKProfile (awareness of other brokers)
// - console.tpl.go: ConsoleConfig (uses ordinals based on replicas)
// - poddisruptionbudget.go: PodDisruptionBudget (calculated based off of single statefulset)
// - service.loadbalancer.go: LoadBalancerServices (uses ordinals based on replicas)
// - ? service.nodeport.go: NodePortService (uses label selector for one statefulset)
// - ? service_internal.go: ServiceInternal (uses label selector for one statefulset)
// - secrets.go: SecretSTSLifecycle (uses replicas for preStopHook bypass), SecretFSValidator (uses StatefulSet.InitContainers and replicas), SecretConfigurator (uses replicas)
// - statefulset.go: no explanation needed

// +gotohelm:ignore=true
func RenderResourcesFromCRD(cfg *kube.RESTConfig, release helmette.Release, cluster *redpandav1alpha3.Redpanda, pools []*redpandav1alpha3.NodePool) (_ []kube.Object, err error) {
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

	manifests := renderResources(dot, pools)
	for _, chart := range dot.Subcharts {
		// we only have a single subchart defined here, this abuses the fact that
		// we won't have a dot context in Subcharts if its not enabled, and, if
		// we do have one, it is Console

		consoleManifests, err := console.Chart.Render(chart.KubeConfig, chart.Release, chart.Values)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, consoleManifests...)
	}

	return manifests, nil
}

// +gotohelm:ignore=true
func RenderNodePoolsFromCRD(cfg *kube.RESTConfig, release helmette.Release, cluster *redpandav1alpha3.Redpanda, pools []*redpandav1alpha3.NodePool) (_ []*appsv1.StatefulSet, err error) {
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
