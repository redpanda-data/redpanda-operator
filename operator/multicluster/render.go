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
	"reflect"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

// Types returns a slice containing the set of all [kube.Object] types that
// could be returned by the stretch cluster renderer.
func Types() []kube.Object {
	return []kube.Object{
		&appsv1.StatefulSet{},
		&certmanagerv1.Certificate{},
		&certmanagerv1.Issuer{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&monitoringv1.ServiceMonitor{},
		&policyv1.PodDisruptionBudget{},
		&rbacv1.ClusterRoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.RoleBinding{},
		&rbacv1.Role{},
	}
}

// RenderNodePools renders all node pool StatefulSets.
func RenderNodePools(state *RenderState) ([]*appsv1.StatefulSet, error) {
	return statefulSets(state)
}

// RenderResources renders non-nodepool resources.
func RenderResources(state *RenderState) ([]kube.Object, error) {
	pdb := podDisruptionBudget(state)

	certs, err := certificates(state)
	if err != nil {
		return nil, err
	}

	secretObjs, err := secrets(state)
	if err != nil {
		return nil, err
	}

	lbServices, err := loadBalancerServices(state)
	if err != nil {
		return nil, err
	}

	cm, err := configMaps(state)
	if err != nil {
		return nil, err
	}

	var manifests []kube.Object
	manifests = appendIfNotNil(manifests, nodePortService(state))
	manifests = appendIfNotNil(manifests, pdb)
	manifests = appendIfNotNil(manifests, serviceAccount(state))
	manifests = appendIfNotNil(manifests, serviceInternal(state))
	manifests = appendIfNotNil(manifests, serviceMonitor(state))
	manifests = appendIfNotNil(manifests, cm...)
	manifests = appendIfNotNil(manifests, certIssuers(state)...)
	manifests = appendIfNotNil(manifests, rootCAs(state)...)
	manifests = appendIfNotNil(manifests, certs...)
	manifests = appendIfNotNil(manifests, roles(state)...)
	manifests = appendIfNotNil(manifests, clusterRoles(state)...)
	manifests = appendIfNotNil(manifests, roleBindings(state)...)
	manifests = appendIfNotNil(manifests, clusterRoleBindings(state)...)
	manifests = appendIfNotNil(manifests, lbServices...)
	manifests = appendIfNotNil(manifests, secretObjs...)

	ppServices, err := perPodServices(state)
	if err != nil {
		return nil, err
	}
	manifests = appendIfNotNil(manifests, ppServices...)

	return manifests, nil
}

// appendIfNotNil filters out nil-valued objects before appending. It uses
// reflect.ValueOf to check for nil because a typed nil pointer (e.g.
// (*corev1.Secret)(nil)) satisfies the kube.Object interface but is not
// a Go nil interface value — a plain `obj == nil` check would miss it.
func appendIfNotNil[T kube.Object](manifests []kube.Object, objs ...T) []kube.Object {
	for _, obj := range objs {
		if !reflect.ValueOf(obj).IsNil() {
			manifests = append(manifests, obj)
		}
	}
	return manifests
}
