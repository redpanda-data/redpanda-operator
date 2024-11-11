// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	cmapiv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	helmControllerAPIv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	helmControllerAPIv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	sourceControllerAPIv1 "github.com/fluxcd/source-controller/api/v1"
	sourceControllerAPIv1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	V1SchemeFns = []func(s *runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		cmapiv1.AddToScheme,
		vectorizedv1alpha1.AddToScheme,
	}
	V2SchemeFns = []func(s *runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		cmapiv1.AddToScheme,
		helmControllerAPIv2beta1.AddToScheme,
		helmControllerAPIv2beta2.AddToScheme,
		redpandav1alpha1.AddToScheme,
		redpandav1alpha2.AddToScheme,
		sourceControllerAPIv1.AddToScheme,
		sourceControllerAPIv1beta2.AddToScheme,
		monitoringv1.AddToScheme,
	}
)

func GetV1Scheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, fn := range V1SchemeFns {
		utilruntime.Must(fn(scheme))
	}

	return scheme
}

func GetV2Scheme() *runtime.Scheme {
	scheme := runtime.NewScheme()

	for _, fn := range V2SchemeFns {
		utilruntime.Must(fn(scheme))
	}

	return scheme
}
