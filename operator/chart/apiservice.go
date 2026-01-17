// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_apiservice.go.tpl
package operator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func APIService(dot *helmette.Dot) *corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Experimental.APIServer.Enabled {
		return nil
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-virtual-server", Name(dot)),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(dot),
			Ports: []corev1.ServicePort{
				{
					Port:       int32(443),
					TargetPort: intstr.FromInt32(9050),
				},
			},
		},
	}
}

func APIServiceCertificate(dot *helmette.Dot) *corev1.Secret {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Experimental.APIServer.Enabled {
		return nil
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-virtual-server-certificate", Name(dot)),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
	}
}

func APIServices(dot *helmette.Dot) []*apiregistrationv1.APIService {
	return []*apiregistrationv1.APIService{
		APIServiceV1Alpha1(dot),
	}
}

func APIServiceV1Alpha1(dot *helmette.Dot) *apiregistrationv1.APIService {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Experimental.APIServer.Enabled {
		return nil
	}

	return &apiregistrationv1.APIService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiregistration.k8s.io/v1",
			Kind:       "APIService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "v1alpha1.virtual.cluster.redpanda.com",
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: apiregistrationv1.APIServiceSpec{
			Group:                "virtual.cluster.redpanda.com",
			GroupPriorityMinimum: 1000,
			VersionPriority:      15,
			Service: &apiregistrationv1.ServiceReference{
				Namespace: dot.Release.Namespace,
				Name:      fmt.Sprintf("%s-virtual-server", Name(dot)),
			},
			Version: "v1alpha1",
		},
	}
}
