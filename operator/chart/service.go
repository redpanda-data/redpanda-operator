// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_service.go.tpl
package operator

import (
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func WebhookService(dot *helmette.Dot) *corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Webhook.Enabled {
		return nil
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-webhook-service", Name(dot)),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(dot),
			Ports: []corev1.ServicePort{
				{
					Port:       int32(443),
					TargetPort: intstr.FromInt32(9443),
				},
			},
		},
	}
}

func MetricsService(dot *helmette.Dot) *corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cleanForK8sWithSuffix(Fullname(dot), "metrics-service"),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: values.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(dot),
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       int32(8443),
					TargetPort: intstr.FromString("https"),
				},
			},
		},
	}
}

func MutatingWebhookConfiguration(dot *helmette.Dot) *admissionregistrationv1.MutatingWebhookConfiguration {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Webhook.Enabled {
		return nil
	}

	return &admissionregistrationv1.MutatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "MutatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-mutating-webhook-configuration", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Annotations: map[string]string{
				"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/redpanda-serving-cert", dot.Release.Namespace),
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      fmt.Sprintf("%s-webhook-service", Name(dot)),
						Namespace: dot.Release.Namespace,
						Path:      ptr.To("/mutate-redpanda-vectorized-io-v1alpha1-cluster"),
					},
				},
				FailurePolicy: ptr.To(admissionregistrationv1.Fail),
				Name:          "mcluster.kb.io",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"redpanda.vectorized.io"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{"clusters"},
						},
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
					},
				},
				SideEffects: ptr.To(admissionregistrationv1.SideEffectClassNone),
			},
		},
	}
}

func ValidatingWebhookConfiguration(dot *helmette.Dot) *admissionregistrationv1.ValidatingWebhookConfiguration {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Webhook.Enabled {
		return nil
	}

	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admissionregistration.k8s.io/v1",
			Kind:       "ValidatingWebhookConfiguration",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-validating-webhook-configuration", Fullname(dot)),
			Namespace: dot.Release.Namespace,
			Annotations: map[string]string{
				"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/redpanda-serving-cert", dot.Release.Namespace),
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				AdmissionReviewVersions: []string{"v1", "v1beta1"},
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      fmt.Sprintf("%s-webhook-service", Name(dot)),
						Namespace: dot.Release.Namespace,
						Path:      ptr.To("/validate-redpanda-vectorized-io-v1alpha1-cluster"),
					},
				},
				FailurePolicy: ptr.To(admissionregistrationv1.Fail),
				Name:          "mcluster.kb.io",
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"redpanda.vectorized.io"},
							APIVersions: []string{"v1alpha1"},
							Resources:   []string{"clusters"},
						},
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
							admissionregistrationv1.Update,
						},
					},
				},
				SideEffects: ptr.To(admissionregistrationv1.SideEffectClassNone),
			},
		},
	}
}
