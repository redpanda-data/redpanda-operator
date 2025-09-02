// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package conversion

import (
	"encoding/json"
	"reflect"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	"github.com/redpanda-data/redpanda-operator/pkg/valuesutil"
)

func convertYAMLArrayNotNil[T any](state *redpanda.RenderState, from *string, to *[]T) (err error) {
	if from == nil {
		return nil
	}

	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrapf(r, "yaml conversion failed")
		default:
			err = errors.Newf("yaml conversion failed: %#v", r)
		}
	}()

	result := helmette.Tpl(state.Dot, *from, state.Dot)
	return yaml.Unmarshal([]byte(result), to)
}

func convertAndAppendYAMLNotNil[T any](state *redpanda.RenderState, from *string, to *[]T) error {
	converted := []T{}
	if err := convertYAMLArrayNotNil(state, from, &converted); err != nil {
		return err
	}
	*to = append(*to, converted...)
	return nil
}

func convertAndAppendJSONNotNil[T any, V any](from []T, to *[]V) error {
	if from == nil {
		return nil
	}

	converted, err := valuesutil.UnmarshalInto[[]V](from)
	if err != nil {
		return err
	}
	*to = append(*to, converted...)
	return nil
}

func convertJSON(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}

func convertJSONNotNil[T any, U *T, V any](from U, to *V) error {
	if from == nil {
		return nil
	}

	return convertJSON(from, to)
}

func containerOrInit(containers *[]applycorev1.ContainerApplyConfiguration, name string) *applycorev1.ContainerApplyConfiguration {
	for i := range *containers {
		container := &(*containers)[i]
		if ptr.Deref(container.Name, "") == name {
			return container
		}
	}
	container := applycorev1.ContainerApplyConfiguration{
		Name: ptr.To(name),
	}
	*containers = append(*containers, container)
	return &(*containers)[len(*containers)-1]
}

type containerSpec interface {
	GetResources() *corev1.ResourceRequirements
	GetExtraVolumeMounts() *string
}

func convertInitContainer(state *redpanda.RenderState, values *redpanda.Values, name string, spec containerSpec) error {
	if reflect.ValueOf(spec).IsNil() {
		return nil
	}

	container := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, name)
	if err := convertJSONNotNil(spec.GetResources(), container.Resources); err != nil {
		return err
	}
	if err := convertAndAppendYAMLNotNil(state, spec.GetExtraVolumeMounts(), &container.VolumeMounts); err != nil {
		return err
	}

	return nil
}

func convertAndInitializeAffinityNotNil(spec *corev1.PodAffinity, affinity *applycorev1.AffinityApplyConfiguration) error {
	if spec == nil {
		return nil
	}

	if affinity.PodAffinity == nil {
		affinity.PodAffinity = &applycorev1.PodAffinityApplyConfiguration{}
	}

	return convertJSON(spec, affinity.PodAffinity)
}

func convertAndInitializeAntiAffinityNotNil(state *redpanda.RenderState, spec *redpandav1alpha2.PodAntiAffinity, affinity *applycorev1.AffinityApplyConfiguration) {
	if spec == nil {
		return
	}

	switch ptr.Deref(spec.Type, "") {
	case "hard":
		affinity.PodAntiAffinity = &applycorev1.PodAntiAffinityApplyConfiguration{
			RequiredDuringSchedulingIgnoredDuringExecution: []applycorev1.PodAffinityTermApplyConfiguration{
				{
					TopologyKey: ptr.To(ptr.Deref(spec.TopologyKey, "kubernetes.io/hostname")),
					LabelSelector: &applymetav1.LabelSelectorApplyConfiguration{
						MatchLabels: redpanda.StatefulSetPodLabelsSelector(state, redpanda.Pool{Statefulset: state.Values.Statefulset}),
					},
				},
			},
		}
	case "soft":
		affinity.PodAntiAffinity = &applycorev1.PodAntiAffinityApplyConfiguration{
			PreferredDuringSchedulingIgnoredDuringExecution: []applycorev1.WeightedPodAffinityTermApplyConfiguration{
				{
					// the default values for weight and topologyKey are taken from the v5 values.yaml
					Weight: ptr.To(int32(ptr.Deref(spec.Weight, 100))),
					PodAffinityTerm: &applycorev1.PodAffinityTermApplyConfiguration{
						TopologyKey: ptr.To(ptr.Deref(spec.TopologyKey, "kubernetes.io/hostname")),
						LabelSelector: &applymetav1.LabelSelectorApplyConfiguration{
							MatchLabels: redpanda.StatefulSetPodLabelsSelector(state, redpanda.Pool{Statefulset: state.Values.Statefulset}),
						},
					},
				},
			},
		}
	case "custom":
		if spec.Custom == nil {
			return
		}
		affinity.PodAntiAffinity = ptr.To(helmette.UnmarshalInto[applycorev1.PodAntiAffinityApplyConfiguration](spec.Custom))
	}
}
