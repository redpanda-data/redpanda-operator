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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// configMaps returns all ConfigMaps for the given RenderState.
func configMaps(state *RenderState) ([]*corev1.ConfigMap, error) {
	var cms []*corev1.ConfigMap
	for _, pool := range state.inClusterPools {
		cm, err := redpandaConfigMap(state, pool)
		if err != nil {
			return nil, err
		}
		cms = append(cms, cm)
	}
	if cm := rpkProfileConfigMap(state); cm != nil {
		cms = append(cms, cm)
	}
	return cms, nil
}

// redpandaConfigMap returns the ConfigMap for a specific pool.
func redpandaConfigMap(state *RenderState, pool *redpandav1alpha2.NodePool) (*corev1.ConfigMap, error) {
	bootstrap := bootstrapContents(state)
	redpanda, err := redpandaConfigFile(state, true, pool)
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.poolFullname(pool),
			Namespace: state.namespace,
			Labels:    state.commonLabels(),
		},
		Data: map[string]string{
			".bootstrap.json.in":    bootstrap.template,
			"bootstrap.yaml.fixups": bootstrap.fixups,
			"redpanda.yaml":         redpanda,
		},
	}, nil
}
