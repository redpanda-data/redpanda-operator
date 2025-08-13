// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_render_state.go.tpl
package redpanda

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/ir"
)

// RenderState contains contextual information about the current rendering of
// the chart.
type RenderState struct {
	// Metadata about the current Helm release.
	Release *helmette.Release
	// Files contains the static files that are part of the Helm chart.
	Files *helmette.Files
	// Chart is the helm chart being rendered.
	Chart *helmette.Chart

	// Values are the values used to render the chart.
	Values Values

	// BootstrapUserSecret is the Secret that may already exist in the cluster.
	BootstrapUserSecret *corev1.Secret
	// BootstrapPassword is the password of the bootstrap user, if it could be found.
	BootstrapUserPassword string

	// StatefulSetPodLabels contains the labels that may already exist for the default statefulset pod template.
	StatefulSetPodLabels map[string]string
	// StatefulSetSelector contains the selector that may already exist for the default statefulset.
	StatefulSetSelector map[string]string

	// Pools contains the list of NodePools that are being rendered.
	Pools []Pool

	// Dot is the underlying [helmette.Dot] that was used to construct this
	// RenderState.
	// TODO: remove this eventually once we get templating figured out.
	Dot *helmette.Dot
}

// FetchBootstrapUser attempts to locate an existing bootstrap user secret in
// the cluster. If found, it is stored in [RenderState.BootstrapUserSecret
func (r *RenderState) FetchBootstrapUser() {
	if r.Values.Auth.SASL == nil || !r.Values.Auth.SASL.Enabled || r.Values.Auth.SASL.BootstrapUser.SecretKeyRef != nil {
		return
	}

	secretName := fmt.Sprintf("%s-bootstrap-user", Fullname(r))

	// Some tools don't correctly set .Release.Upgrade (ArgoCD, gotohelm, helm
	// template) which has lead us to incorrectly re-generate the bootstrap
	// user password. Rather than gating, we always attempt a lookup as that's
	// likely the safest option. Though it's likely that Lookup will be
	// stubbed out in similar scenarios (helm template).
	// TODO: Should we try to detect invalid configurations, panic, and request
	// that a password be explicitly set?
	// See also: https://github.com/redpanda-data/helm-charts/issues/1596
	if existing, ok := helmette.Lookup[corev1.Secret](r.Dot, r.Release.Namespace, secretName); ok {
		// make any existing secret immutable
		existing.Immutable = ptr.To(true)
		r.BootstrapUserSecret = existing
		selector := r.Values.Auth.SASL.BootstrapUser.SecretKeySelector(Fullname(r))
		if data, found := existing.Data[selector.Key]; found {
			r.BootstrapUserPassword = string(data)
		}
	}
}

// FetchStatefulSetPodSelector attempts to locate an existing statefulset pod
// selector in the cluster. If found, it is stored in [RenderState.StatefulSetPodLabels
func (r *RenderState) FetchStatefulSetPodSelector() {
	// TODO: this may be broken now that we no longer fully distinguish between upgrades/installs
	// in controller applies
	// StatefulSets cannot change their selector. Use the existing one even if it's broken.
	// New installs will get better selectors.
	if r.Release.IsUpgrade {
		if existing, ok := helmette.Lookup[appsv1.StatefulSet](r.Dot, r.Release.Namespace, Fullname(r)); ok && len(existing.Spec.Template.ObjectMeta.Labels) > 0 {
			r.StatefulSetPodLabels = existing.Spec.Template.ObjectMeta.Labels
			r.StatefulSetSelector = existing.Spec.Selector.MatchLabels
		}
	}
}

func (r *RenderState) AsStaticConfigSource() ir.StaticConfigurationSource {
	username := r.Values.Auth.SASL.BootstrapUser.Username()
	passwordRef := r.Values.Auth.SASL.BootstrapUser.SecretKeySelector(Fullname(r))

	// Kafka API configuration
	kafkaSpec := &ir.KafkaAPISpec{
		Brokers: BrokerList(r, r.Values.Listeners.Kafka.Port),
	}

	// Add TLS configuration for Kafka if enabled
	if r.Values.Listeners.Kafka.TLS.IsEnabled(&r.Values.TLS) {
		kafkaSpec.TLS = r.Values.Listeners.Kafka.TLS.ToCommonTLS(r, &r.Values.TLS)
	}

	// TODO This check may need to be more complex.
	// There's two cluster configs and then listener level configuration.
	// Add SASL authentication using bootstrap user if enabled
	if r.Values.Auth.IsSASLEnabled() {
		kafkaSpec.SASL = &ir.KafkaSASL{
			Username: username,
			Password: ir.SecretKeyRef{
				Namespace: r.Release.Namespace,
				Name:      passwordRef.Name,
				Key:       passwordRef.Key,
			},
			Mechanism: ir.SASLMechanism(r.Values.Auth.SASL.BootstrapUser.GetMechanism()),
		}
	}

	// Admin API configuration
	var adminTLS *ir.CommonTLS
	adminSchema := "http"
	if r.Values.Listeners.Admin.TLS.IsEnabled(&r.Values.TLS) {
		adminSchema = "https"
		adminTLS = r.Values.Listeners.Admin.TLS.ToCommonTLS(r, &r.Values.TLS)
	}

	var adminAuth *ir.AdminAuth
	adminAuthEnabled, _ := r.Values.Config.Cluster["admin_api_require_auth"].(bool)
	if adminAuthEnabled {
		adminAuth = &ir.AdminAuth{
			Username: username,
			Password: ir.SecretKeyRef{
				Namespace: r.Release.Namespace,
				Name:      passwordRef.Name,
				Key:       passwordRef.Key,
			},
		}
	}

	adminSpec := &ir.AdminAPISpec{
		TLS:  adminTLS,
		Auth: adminAuth,
		URLs: []string{
			// NB: Console uses SRV based service discovery and doesn't require a full list of addresses.
			fmt.Sprintf("%s://%s:%d", adminSchema, InternalDomain(r), r.Values.Listeners.Admin.Port),
		},
	}

	// Schema Registry configuration (if enabled)
	var schemaRegistrySpec *ir.SchemaRegistrySpec
	if r.Values.Listeners.SchemaRegistry.Enabled {
		var schemaTLS *ir.CommonTLS
		schemaSchema := "http"
		if r.Values.Listeners.SchemaRegistry.TLS.IsEnabled(&r.Values.TLS) {
			schemaSchema = "https"
			schemaTLS = r.Values.Listeners.SchemaRegistry.TLS.ToCommonTLS(r, &r.Values.TLS)
		}

		var schemaURLs []string
		brokers := BrokerList(r, r.Values.Listeners.SchemaRegistry.Port)
		for _, broker := range brokers {
			schemaURLs = append(schemaURLs, fmt.Sprintf("%s://%s", schemaSchema, broker))
		}

		schemaRegistrySpec = &ir.SchemaRegistrySpec{
			URLs: schemaURLs,
			TLS:  schemaTLS,
		}

		// TODO: This check is likely incorrect but it matches the historical
		// behavior.
		if r.Values.Auth.IsSASLEnabled() {
			schemaRegistrySpec.SASL = &ir.SchemaRegistrySASL{
				Username: username,
				Password: ir.SecretKeyRef{
					Namespace: r.Release.Namespace,
					Name:      passwordRef.Name,
					Key:       passwordRef.Key,
				},
			}
		}
	}

	return ir.StaticConfigurationSource{
		Kafka:          kafkaSpec,
		Admin:          adminSpec,
		SchemaRegistry: schemaRegistrySpec,
	}
}
