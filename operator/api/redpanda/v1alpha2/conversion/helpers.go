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
	corev1 "k8s.io/api/core/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func convertJSON(from, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}

func convertYAMLArrayNotNil[T any](dot *helmette.Dot, from *string, to *[]T) (err error) {
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

	result := helmette.Tpl(dot, *from, dot)
	*to = helmette.UnmarshalYamlArray[T](result)
	return
}

func convertAndAppendYAMLNotNil[T any](dot *helmette.Dot, from *string, to *[]T) error {
	converted := []T{}
	if err := convertYAMLArrayNotNil(dot, from, &converted); err != nil {
		return err
	}
	*to = append(*to, converted...)
	return nil
}

func convertAndInitializeJSONNotNil[T any, U *T, V any, W *V](from U, to W) error {
	if from == nil {
		return nil
	}

	if to == nil {
		var v V
		to = &v
	}

	return convertJSON(from, to)
}

func convertAndAppendJSONNotNil[T any, V any](from []T, to *[]V) error {
	if from == nil {
		return nil
	}

	converted := []V{}
	if err := convertJSON(from, &converted); err != nil {
		return err
	}
	*to = append(*to, converted...)
	return nil
}

func convertJSONNotNil[T any, U *T](from U, to any) error {
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

func convertInitContainer(dot *helmette.Dot, values *redpanda.Values, name string, spec containerSpec) error {
	if reflect.ValueOf(spec).IsNil() {
		return nil
	}

	container := containerOrInit(&values.Statefulset.PodTemplate.Spec.InitContainers, name)
	if err := convertJSONNotNil(spec.GetResources(), container.Resources); err != nil {
		return err
	}
	if err := convertAndAppendYAMLNotNil(dot, spec.GetExtraVolumeMounts(), &container.VolumeMounts); err != nil {
		return err
	}

	return nil
}
