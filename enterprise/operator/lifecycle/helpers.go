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
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// sortRevisions sorts a statefulset's controlerRevisions by revision number
func sortRevisions(controllerRevisions []*appsv1.ControllerRevision) []*appsv1.ControllerRevision {
	// from https://github.com/kubernetes/kubernetes/blob/dd25c6a6cb4ea0be1e304de35de45adeef78b264/pkg/controller/history/controller_history.go#L158
	sort.SliceStable(controllerRevisions, func(i, j int) bool {
		if controllerRevisions[i].Revision == controllerRevisions[j].Revision {
			if controllerRevisions[j].CreationTimestamp.Equal(&controllerRevisions[i].CreationTimestamp) {
				return controllerRevisions[i].Name < controllerRevisions[j].Name
			}
			return controllerRevisions[j].CreationTimestamp.After(controllerRevisions[i].CreationTimestamp.Time)
		}
		return controllerRevisions[i].Revision < controllerRevisions[j].Revision
	})

	return controllerRevisions
}

// sortPodsByOrdinal sorts a list of pods by their ordinals, wrapping them
// in a helper container to easily fetch the ordinal subsequently.
func sortPodsByOrdinal(pods ...*corev1.Pod) ([]*podsWithOrdinals, error) {
	withOrdinals := []*podsWithOrdinals{}
	for _, pod := range pods {
		ordinal, err := extractOrdinal(pod.GetName())
		if err != nil {
			return nil, err
		}
		withOrdinals = append(withOrdinals, &podsWithOrdinals{
			ordinal: ordinal,
			pod:     pod.DeepCopy(),
		})
	}

	sort.SliceStable(withOrdinals, func(i, j int) bool {
		return withOrdinals[i].ordinal < withOrdinals[j].ordinal
	})

	return withOrdinals, nil
}

type clusterObject interface {
	GetNamespace() string
	GetName() string
	GetCluster() string
	GetCanonicalClusterName() string
}

func clusterObjectKey(o clusterObject) string {
	return fmt.Sprintf("%s/%s/%s", o.GetCanonicalClusterName(), o.GetNamespace(), o.GetName())
}

// sortByNameAndCluster sorts a generic list of client.Objects by the combination
// of their namespace/name.
func sortByNameAndCluster[T clusterObject](objs []T) []T {
	slices.SortStableFunc(objs, func(a, b T) int {
		return strings.Compare(clusterObjectKey(a), clusterObjectKey(b))
	})

	return objs
}

// extractOrdinal extracts an ordinal from the pod name by parsing the last
// value after a "-" in the pod name
func extractOrdinal(name string) (int, error) {
	resourceTokens := strings.Split(name, "-")
	if len(resourceTokens) < 2 {
		return 0, fmt.Errorf("invalid resource name for ordinal fetching: %s", name)
	}

	// grab the last item after the "-"" which should be the ordinal and parse it
	ordinal, err := strconv.Atoi(resourceTokens[len(resourceTokens)-1])
	if err != nil {
		return 0, fmt.Errorf("parsing resource name %q: %w", name, err)
	}

	return ordinal, nil
}
