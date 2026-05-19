// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package controller

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	mcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	redpandav1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha1"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

var (
	v1SchemeFns = []func(s *runtime.Scheme) error{
		apiextensionsv1.AddToScheme,
		certmanagerv1.AddToScheme,
		clientgoscheme.AddToScheme,
		vectorizedv1alpha1.Install,
	}
	v2SchemeFns = []func(s *runtime.Scheme) error{
		apiextensionsv1.AddToScheme,
		certmanagerv1.AddToScheme,
		clientgoscheme.AddToScheme,
		monitoringv1.AddToScheme,
		// Gateway API v1 — registers the typed List kinds the controller-runtime
		// cache needs to List/Watch the gateway objects we render: TLSRoute (from
		// the redpanda chart) and HTTPRoute (from the Console chart/CRD). A single
		// gatewayv1.Install covers both; it is what lets the cache issue List
		// calls for these kinds on every reconcile pass.
		gatewayv1.Install,
		redpandav1alpha1.Install,
		redpandav1alpha2.Install,
	}
	multiclusterSchemeFns = []func(s *runtime.Scheme) error{
		apiextensionsv1.AddToScheme,
		certmanagerv1.AddToScheme,
		clientgoscheme.AddToScheme,
		redpandav1alpha2.Install,
		monitoringv1.AddToScheme,
		// Register gatewayv1 (HTTPRoute) in the multicluster scheme too — the
		// multicluster Console controller references HTTPRoute exactly like the
		// v2 controller. The Gateway API CRDs are optional: skipWatchIfNotInstalled
		// and the renderer skip HTTPRoute when the CRD is absent, but that graceful
		// skip relies on a NoKindMatchError, which only surfaces when the type is
		// in the scheme. Without this, HTTPRoute ops fail with a serialization
		// error instead and the Console reconcile never completes (no Deployment) —
		// same reasoning as monitoringv1 above for ServiceMonitor.
		gatewayv1.Install,
		mcsv1alpha1.Install,
	}

	MulticlusterScheme *runtime.Scheme
	V1Scheme           *runtime.Scheme
	V2Scheme           *runtime.Scheme
	UnifiedScheme      *runtime.Scheme
)

func init() {
	MulticlusterScheme = runtime.NewScheme()
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

	for _, fn := range multiclusterSchemeFns {
		utilruntime.Must(fn(MulticlusterScheme))
	}
}
