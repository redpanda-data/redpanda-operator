// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package redpanda defines Webhooks for redpanda API group
package redpanda

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/vectorized/v1alpha1"
	consolepkg "github.com/redpanda-data/redpanda-operator/src/go/k8s/pkg/console"
)

// +kubebuilder:webhook:path=/validate-redpanda-vectorized-io-v1alpha1-console,mutating=false,failurePolicy=fail,sideEffects=None,groups="redpanda.vectorized.io",resources=consoles,verbs=create;update,versions=v1alpha1,name=vconsole.kb.io,admissionReviewVersions=v1

// validHostnameSegment matches valid DNS name segments.
var validHostnameSegment = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])$`)

// ConsoleValidator validates Consoles
type ConsoleValidator struct {
	Client  client.Client
	Decoder *admission.Decoder
}

// Handle processes admission for Console
func (v *ConsoleValidator) Handle(
	ctx context.Context,
	req admission.Request, //nolint:gocritic // interface not require pointer
) admission.Response {
	console := &vectorizedv1alpha1.Console{}

	err := v.Decoder.Decode(req, console)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if console.DeletionTimestamp != nil {
		return admission.Allowed("")
	}

	if !console.IsAllowedNamespace() {
		return admission.Denied(fmt.Sprintf("cluster %s/%s is in different namespace", console.Spec.ClusterRef.Namespace, console.Spec.ClusterRef.Name))
	}

	errs, err := ValidatePrometheus(ctx, v.Client, console)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	if len(errs) > 0 {
		return admission.Errored(http.StatusBadRequest, apierrors.NewInvalid(
			console.GroupVersionKind().GroupKind(),
			console.Name, errs))
	}

	// Admit console even if cluster is not yet configured, controller will do backoff retries
	// No checks on referenced cluster if console is deleting so controller can remove finalizers
	cluster := &vectorizedv1alpha1.Cluster{}
	if err := v.Client.Get(ctx, console.GetClusterRef(), cluster); err != nil && console.GetDeletionTimestamp() == nil {
		if apierrors.IsNotFound(err) {
			return admission.Denied(fmt.Sprintf("cluster %s/%s not found", console.Spec.ClusterRef.Namespace, console.Spec.ClusterRef.Name))
		}
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := ValidateEnterpriseRBAC(ctx, v.Client, console); err != nil {
		if errors.Is(err, &ErrKeyNotFound{}) {
			return admission.Denied(err.Error())
		}
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := ValidateEnterpriseGoogleClientCredentials(ctx, v.Client, console); err != nil {
		if errors.Is(err, &ErrKeyNotFound{}) {
			return admission.Denied(err.Error())
		}
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := ValidateEnterpriseGoogleSA(ctx, v.Client, console); err != nil {
		if errors.Is(err, &ErrKeyNotFound{}) {
			return admission.Denied(err.Error())
		}
		return admission.Errored(http.StatusBadRequest, err)
	}

	if console.Spec.Ingress != nil && console.Spec.Ingress.Endpoint != "" && !validHostnameSegment.MatchString(console.Spec.Ingress.Endpoint) {
		return admission.Denied(fmt.Sprintf("ingress endpoint does not match regex %s", validHostnameSegment.String()))
	}

	return admission.Allowed("")
}

// +kubebuilder:webhook:path=/mutate-redpanda-vectorized-io-v1alpha1-console,mutating=true,failurePolicy=fail,sideEffects=None,groups="redpanda.vectorized.io",resources=consoles,verbs=create;update,versions=v1alpha1,name=mconsole.kb.io,admissionReviewVersions=v1

// ConsoleDefaulter mutates Consoles
type ConsoleDefaulter struct {
	Client  client.Client
	Decoder *admission.Decoder
}

// Handle processes admission for Console
func (m *ConsoleDefaulter) Handle(
	ctx context.Context,
	req admission.Request, //nolint:gocritic // interface not require pointer
) admission.Response {
	console := &vectorizedv1alpha1.Console{}

	err := m.Decoder.Decode(req, console)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	response, err := m.Default(console)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return *response
}

// Default implements admission defaulting
func (m *ConsoleDefaulter) Default(
	console *vectorizedv1alpha1.Console,
) (*admission.Response, error) {
	original, err := json.Marshal(console.DeepCopy())
	if err != nil {
		return nil, err
	}

	if login := console.Spec.Login; login != nil && login.JWTSecretRef.Key == "" {
		login.JWTSecretRef.Key = consolepkg.DefaultJWTSecretKey
	}
	if license := console.Spec.LicenseRef; license != nil && license.Key == "" {
		license.Key = vectorizedv1alpha1.DefaultLicenseSecretKey
	}
	if console.Spec.Cloud != nil &&
		console.Spec.Cloud.PrometheusEndpoint != nil &&
		console.Spec.Cloud.PrometheusEndpoint.ResponseCacheDuration == nil {
		console.Spec.Cloud.PrometheusEndpoint.ResponseCacheDuration = &metav1.Duration{Duration: 1 * time.Second}
	}

	if console.Spec.Cloud != nil &&
		console.Spec.Cloud.PrometheusEndpoint != nil &&
		console.Spec.Cloud.PrometheusEndpoint.Prometheus != nil &&
		console.Spec.Cloud.PrometheusEndpoint.Prometheus.TargetRefreshInterval == nil {
		console.Spec.Cloud.PrometheusEndpoint.Prometheus.TargetRefreshInterval = &metav1.Duration{Duration: 10 * time.Second}
	}

	current, err := json.Marshal(console)
	if err != nil {
		return nil, err
	}
	response := admission.PatchResponseFromRaw(original, current)
	return &response, nil
}
