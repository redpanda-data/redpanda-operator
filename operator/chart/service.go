// Copyright 2026 Redpanda Data, Inc.
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
	mcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// OperatorServicePort is the gRPC port the operator listens on for
// cross-cluster raft traffic. Matches the --raft-address default.
const OperatorServicePort = 9443

// OperatorService renders the peer-facing gRPC Service in front of the
// operator Deployment when multicluster.service.enabled is true. The
// Service type and annotations come straight from values — any mesh
// that configures routing via annotations (Cilium ClusterMesh,
// Submariner, …) or any cloud-specific LoadBalancer knob fits without
// the chart hard-coding any particular implementation.
//
// The Service name equals the helm fullname so peers can address it as
// `<fullname>.<namespace>.svc.cluster.local:9443`. That matches the
// naming convention the bootstrap tooling already uses for the TLS
// secret prefix.
func OperatorService(dot *helmette.Dot) *corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Multicluster.Enabled || !values.Multicluster.Service.Enabled {
		return nil
	}

	svcType := values.Multicluster.Service.Type
	if svcType == "" {
		svcType = corev1.ServiceTypeClusterIP
	}

	annotations := helmette.Merge(
		map[string]string{},
		helmette.Default(map[string]string{}, values.Annotations),
		helmette.Default(map[string]string{}, values.Multicluster.Service.Annotations),
	)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        Fullname(dot),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: SelectorLabels(dot),
			Ports: []corev1.ServicePort{
				{
					Name:       "raft",
					Port:       int32(OperatorServicePort),
					TargetPort: intstr.FromInt32(int32(OperatorServicePort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			// Peers need to reach this Service before the local operator
			// is Ready (Ready is gated on raft quorum forming), so don't
			// wait for readiness to publish endpoint addresses.
			PublishNotReadyAddresses: true,
		},
	}
}

// OperatorPeerServices renders a selectorless placeholder Service for
// every remote peer listed in multicluster.peers (excluding the local
// cluster itself). Rendered only when multicluster.service.mesh=true.
//
// Why: with Cilium ClusterMesh "global services", a Service named X
// on cluster A merges its endpoints into a Service also named X on
// cluster B — but only if both Services exist. Without the placeholder
// on cluster B, a pod on B looking up `X.<ns>.svc.cluster.local` gets
// NXDOMAIN. This function emits the placeholders so every cluster can
// resolve every peer's operator Service. The real endpoints come from
// the OperatorService on the peer's own cluster; ClusterMesh merges
// them in via the matching name.
//
// Placeholders are always ClusterIP — they carry no selector and
// therefore no local endpoints, so a LoadBalancer type here would
// provision a cloud LB with nothing behind it. Only the local
// OperatorService respects Multicluster.Service.Type.
//
// Annotations are Multicluster.Service.Annotations merged with the
// peer's own Peer.Annotations (peer wins on conflict), so a user can
// set mesh-wide defaults and peer-specific overrides like Cilium
// `service.cilium.io/affinity: <cluster-name>`.
func OperatorPeerServices(dot *helmette.Dot) []corev1.Service {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Multicluster.Enabled || !values.Multicluster.Service.Enabled {
		return nil
	}
	if !values.Multicluster.Service.Mesh {
		return nil
	}

	self := values.Multicluster.Name
	var svcs []corev1.Service
	for _, p := range values.Multicluster.Peers {
		if p.Name == self {
			continue
		}
		annotations := helmette.Merge(
			map[string]string{},
			helmette.Default(map[string]string{}, values.Annotations),
			helmette.Default(map[string]string{}, values.Multicluster.Service.Annotations),
			helmette.Default(map[string]string{}, p.Annotations),
		)
		svcs = append(svcs, corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        p.Name,
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				// No selector → no local endpoints. Cilium ClusterMesh
				// merges in the remote peer's endpoints.
				Ports: []corev1.ServicePort{
					{
						Name:       "raft",
						Port:       int32(OperatorServicePort),
						TargetPort: intstr.FromInt32(int32(OperatorServicePort)),
						Protocol:   corev1.ProtocolTCP,
					},
				},
			},
		})
	}
	return svcs
}

// OperatorServiceExport renders an MCS ServiceExport for the operator
// Service when multicluster.service.mcs is true. A compliant MCS
// controller in the cluster mirrors the Service into every peer cluster
// under `<name>.<namespace>.svc.clusterset.local`.
func OperatorServiceExport(dot *helmette.Dot) *mcsv1alpha1.ServiceExport {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Multicluster.Enabled ||
		!values.Multicluster.Service.Enabled ||
		!values.Multicluster.Service.MCS {
		return nil
	}

	return &mcsv1alpha1.ServiceExport{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "multicluster.x-k8s.io/v1alpha1",
			Kind:       "ServiceExport",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        Fullname(dot),
			Namespace:   dot.Release.Namespace,
			Labels:      Labels(dot),
			Annotations: helmette.Default(map[string]string{}, values.Annotations),
		},
	}
}

// OperatorServiceImports renders one ServiceImport per remote peer
// when multicluster.service.mcs is true. Each import gives this
// cluster a local clusterset-scoped entry point for that peer's
// exported operator Service, resolvable at
// `<peer>.<namespace>.svc.clusterset.local`.
//
// The local cluster is skipped: its own ServiceExport causes the MCS
// controller to auto-create a matching ServiceImport on every cluster
// in the clusterset including this one, so a chart-managed import for
// self would collide with the controller-managed one.
//
// Peers are trusted to be named after each remote operator's helm
// fullname — the same convention the chart uses elsewhere for matching
// peer.Name against the remote cluster's multicluster.name.
func OperatorServiceImports(dot *helmette.Dot) []mcsv1alpha1.ServiceImport {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Multicluster.Enabled ||
		!values.Multicluster.Service.Enabled ||
		!values.Multicluster.Service.MCS {
		return nil
	}

	self := values.Multicluster.Name
	var imports []mcsv1alpha1.ServiceImport
	for _, p := range values.Multicluster.Peers {
		if p.Name == self {
			continue
		}
		imports = append(imports, mcsv1alpha1.ServiceImport{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "multicluster.x-k8s.io/v1alpha1",
				Kind:       "ServiceImport",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        p.Name,
				Namespace:   dot.Release.Namespace,
				Labels:      Labels(dot),
				Annotations: helmette.Default(map[string]string{}, values.Annotations),
			},
			Spec: mcsv1alpha1.ServiceImportSpec{
				Type: mcsv1alpha1.ClusterSetIP,
				Ports: []mcsv1alpha1.ServicePort{
					{
						Name:     "raft",
						Protocol: corev1.ProtocolTCP,
						Port:     int32(OperatorServicePort),
					},
				},
			},
		})
	}
	return imports
}

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
				"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", dot.Release.Namespace, CertificateName(dot)),
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
				"cert-manager.io/inject-ca-from": fmt.Sprintf("%s/%s", dot.Release.Namespace, CertificateName(dot)),
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
