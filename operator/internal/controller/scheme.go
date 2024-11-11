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
	v1SchemeFns = []func(s *runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		cmapiv1.AddToScheme,
		vectorizedv1alpha1.AddToScheme,
	}
	v2SchemeFns = []func(s *runtime.Scheme) error{
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

	V1Scheme      *runtime.Scheme
	V2Scheme      *runtime.Scheme
	UnifiedScheme *runtime.Scheme
)

func init() {
	V1Scheme = runtime.NewScheme()
	V2Scheme = runtime.NewScheme()
	UnifiedScheme = runtime.NewScheme()

	for _, fn := range v1SchemeFns {
		utilruntime.Must(fn(V1Scheme))
		utilruntime.Must(fn(UnifiedScheme))
	}

	for _, fn := range v2SchemeFns {
		utilruntime.Must(fn(V2Scheme))
		utilruntime.Must(fn(UnifiedScheme))
	}
}
