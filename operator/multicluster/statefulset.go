// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/kube/podtemplate"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/tplutil"
)

// statefulSets returns all StatefulSets for the given RenderState.
func statefulSets(state *RenderState) ([]*appsv1.StatefulSet, error) {
	var sets []*appsv1.StatefulSet
	for _, pool := range state.inClusterPools {
		ss, err := statefulSet(state, pool)
		if err != nil {
			return nil, err
		}
		sets = append(sets, ss)
	}
	return sets, nil
}

// statefulSet returns the StatefulSet for the given pool.
func statefulSet(state *RenderState, pool *redpandav1alpha2.NodePool) (*appsv1.StatefulSet, error) {
	poolLabels := map[string]string{}
	if pool.Name != "" {
		poolLabels[nodePoolLabelName] = pool.Name
		poolLabels[nodePoolLabelGeneration] = fmt.Sprintf("%d", pool.Generation)
	}

	labels := state.commonLabels()
	labels[labelComponentKey] = fmt.Sprintf("redpanda%s", pool.Suffix())
	for k, v := range poolLabels {
		labels[k] = v
	}

	selectorLabels := statefulSetPodLabelsSelector(state, pool)

	checksum, err := statefulSetChecksumAnnotation(state, pool)
	if err != nil {
		return nil, err
	}

	podTemplate := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: statefulSetPodLabels(state, pool),
			Annotations: map[string]string{
				"config.redpanda.com/checksum": checksum,
			},
		},
		Spec: corev1.PodSpec{
			ImagePullSecrets:              state.Spec().ImagePullSecrets,
			AutomountServiceAccountToken:  ptr.To(false),
			ServiceAccountName:            state.Spec().GetServiceAccountName(state.fullname()),
			TerminationGracePeriodSeconds: ptr.To(defaultTerminationGracePeriod),
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup:             ptr.To(redpandaUserID),
				RunAsUser:           ptr.To(redpandaUserID),
				FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
			},
			// Hard anti-affinity: never co-locate two pods from the same pool on one node.
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: selectorLabels,
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
			// Soft zone spread: prefer even distribution across zones, but don't
			// block scheduling if zones are unbalanced (ScheduleAnyway).
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: corev1.ScheduleAnyway,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: selectorLabels,
					},
				},
			},
			InitContainers: statefulSetInitContainers(state, pool),
			Containers:     statefulSetContainers(state, pool),
			Volumes:        statefulSetVolumes(state, pool),
		},
	}

	// Apply pool-level PodTemplate overrides.
	if pool.Spec.PodTemplate != nil {
		expanded, err := structuredTpl(state, *pool.Spec.PodTemplate)
		if err != nil {
			return nil, fmt.Errorf("expanding pod template: %w", err)
		}
		merged, err := podtemplate.StrategicMergePatch(podtemplate.Overrides{
			Labels:      expanded.Labels,
			Annotations: expanded.Annotations,
			Spec:        expanded.Spec,
		}, podTemplate)
		if err != nil {
			return nil, fmt.Errorf("applying pod template overrides: %w", err)
		}
		podTemplate = merged
	}

	set := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.poolFullname(pool),
			Namespace: state.namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: statefulSetPodLabelsSelector(state, pool),
			},
			ServiceName:         state.Spec().GetServiceName(state.fullname()),
			Replicas:            ptr.To(pool.GetReplicas()),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			// OnDelete lets the operator control rollout ordering (e.g.
			// maintenance mode drain → restart → clear maintenance) rather
			// than letting the StatefulSet controller do a blind rolling update.
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template: podTemplate,
		},
	}

	// VolumeClaimTemplates for persistent storage.
	if storage := state.Spec().Storage; storage != nil && storage.PersistentVolume.IsEnabled() {
		if t := volumeClaimTemplateDatadir(state); t != nil {
			set.Spec.VolumeClaimTemplates = append(set.Spec.VolumeClaimTemplates, *t)
		}
	}
	if t := volumeClaimTemplateTieredStorageDir(state); t != nil {
		set.Spec.VolumeClaimTemplates = append(set.Spec.VolumeClaimTemplates, *t)
	}

	return set, nil
}

// statefulSetPodLabelsSelector returns the label selector for the Redpanda StatefulSet.
func statefulSetPodLabelsSelector(state *RenderState, pool *redpandav1alpha2.NodePool) map[string]string {
	// StatefulSet selectors are immutable after creation. For the default
	// (unnamed) pool, reuse whatever selector was deployed — even if it
	// doesn't match the current label computation — to avoid API rejections.
	// Named pools are new and always get fresh selectors.
	if state.statefulSetSelector != nil && pool.Name == "" {
		return state.statefulSetSelector
	}

	component := fmt.Sprintf("%s-statefulset",
		strings.TrimSuffix(tplutil.Trunc(51, fmt.Sprintf("redpanda%s", pool.Suffix())), "-"))

	defaults := map[string]string{
		labelComponentKey: component,
	}

	result := state.clusterPodLabelsSelector()
	for k, v := range defaults {
		result[k] = v
	}

	// Merge pool-specific additional selector labels.
	for k, v := range pool.Spec.AdditionalSelectorLabels {
		result[k] = v
	}

	return result
}

// statefulSetPodLabels returns the labels for the Redpanda PodTemplate.
func statefulSetPodLabels(state *RenderState, pool *redpandav1alpha2.NodePool) map[string]string {
	if state.statefulSetPodLabels != nil && pool.Name == "" {
		return state.statefulSetPodLabels
	}

	labels := statefulSetPodLabelsSelector(state, pool)
	labels[labelPDBKey] = state.fullname()
	labels[labelBrokerKey] = "true"

	for k, v := range state.commonLabels() {
		if _, exists := labels[k]; !exists {
			labels[k] = v
		}
	}

	return labels
}

// statefulSetContainers returns the containers for the StatefulSet.
func statefulSetContainers(state *RenderState, pool *redpandav1alpha2.NodePool) []corev1.Container {
	var containers []corev1.Container
	containers = append(containers, statefulSetContainerRedpanda(state, pool))
	containers = append(containers, statefulSetContainerSidecar(state, pool))
	return containers
}

// statefulSetChecksumAnnotation computes a SHA256 checksum of the rendered config
// to trigger rolling restarts when the configuration changes.
func statefulSetChecksumAnnotation(state *RenderState, pool *redpandav1alpha2.NodePool) (string, error) {
	var dependencies []any
	// NB: Seed servers are excluded to avoid a rolling restart when only
	// replicas are changed.
	redpanda, err := redpandaConfigFile(state, false, pool)
	if err != nil {
		return "", err
	}
	dependencies = append(dependencies, redpanda)
	if state.Spec().External.IsEnabled() {
		dependencies = append(dependencies, ptr.Deref(state.Spec().External.Domain, ""))
		if state.Spec().External.Addresses == nil || len(state.Spec().External.Addresses) == 0 {
			dependencies = append(dependencies, "")
		} else {
			dependencies = append(dependencies, state.Spec().External.Addresses)
		}
	}
	data, _ := json.Marshal(dependencies)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
