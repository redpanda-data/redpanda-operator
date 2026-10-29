// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func ConfigMap(state *RenderState) *corev1.ConfigMap {
	if !state.Values.ConfigMap.Create {
		return nil
	}

	data := map[string]string{
		"config.yaml": fmt.Sprintf("# from .Values.config\n%s\n", state.Template(helmette.ToYaml(state.Values.Config))),
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    state.Labels(nil),
			Name:      state.FullName(),
			Namespace: state.Namespace,
		},
		Data: data,
	}
}
