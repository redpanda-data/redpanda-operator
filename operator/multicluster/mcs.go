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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
)

// serviceExports returns ServiceExport resources for local per-pod Services
// in MCS mode. Exporting a Service makes it discoverable via the
// clusterset.local DNS domain by remote clusters.
func serviceExports(state *RenderState) []*mcsv1alpha1.ServiceExport {
	if !state.Spec().Networking.IsMCS() {
		return nil
	}

	var exports []*mcsv1alpha1.ServiceExport
	for _, pool := range state.pools {
		if !state.isLocalPool(pool) {
			continue
		}
		for i := int32(0); i < pool.GetReplicas(); i++ {
			name := PerPodServiceName(pool, i)
			exports = append(exports, &mcsv1alpha1.ServiceExport{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multicluster.x-k8s.io/v1alpha1",
					Kind:       "ServiceExport",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: state.namespace,
					Labels:    state.commonLabels(),
				},
			})
		}
	}
	return exports
}

// serviceImports returns ServiceImport resources for remote per-pod Services
// in MCS mode. Importing a Service from another cluster creates a local
// virtual service backed by the MCS controller.
func serviceImports(state *RenderState) []*mcsv1alpha1.ServiceImport {
	if !state.Spec().Networking.IsMCS() {
		return nil
	}

	var imports []*mcsv1alpha1.ServiceImport
	for _, pool := range state.pools {
		if state.isLocalPool(pool) {
			continue
		}
		for i := int32(0); i < pool.GetReplicas(); i++ {
			name := PerPodServiceName(pool, i)
			ports := perPodServicePorts(state.Spec())
			var importPorts []mcsv1alpha1.ServicePort
			for _, p := range ports {
				importPorts = append(importPorts, mcsv1alpha1.ServicePort{
					Name:     p.Name,
					Protocol: p.Protocol,
					Port:     p.Port,
				})
			}
			imports = append(imports, &mcsv1alpha1.ServiceImport{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "multicluster.x-k8s.io/v1alpha1",
					Kind:       "ServiceImport",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: state.namespace,
					Labels:    state.commonLabels(),
				},
				Spec: mcsv1alpha1.ServiceImportSpec{
					Type:  mcsv1alpha1.ClusterSetIP,
					Ports: importPorts,
				},
			})
		}
	}
	return imports
}
