// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package operator exposes top-level wiring helpers (the multicluster runtime
// scheme) shared across the enterprise packages and their tests. It lives at
// the module's operator/ root so it can be imported by the lifecycle package's
// tests without forming an import cycle. It mirrors the multicluster scheme in
// the OSS operator's internal/controller package, minus the OSS-only API
// groups (the OSS v1alpha2 Redpanda kinds and Gateway API) that only OSS
// controllers reconcile.
package operator

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	mcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/enterprise/operator/api/redpanda/v1alpha2"
)

// Only the multicluster scheme is carried in the enterprise module; the OSS
// operator owns the v1/v2 schemes.
var (
	multiclusterSchemeFns = []func(s *runtime.Scheme) error{
		apiextensionsv1.AddToScheme,
		certmanagerv1.AddToScheme,
		clientgoscheme.AddToScheme,
		redpandav1alpha2.Install,
		monitoringv1.AddToScheme,
		mcsv1alpha1.Install,
	}

	MulticlusterScheme *runtime.Scheme
)

func init() {
	MulticlusterScheme = runtime.NewScheme()

	for _, fn := range multiclusterSchemeFns {
		utilruntime.Must(fn(MulticlusterScheme))
	}
}
