// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_secrets.go.tpl
package chart

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

const DefaultSASLMechanism = SASLMechanism("SCRAM-SHA-512")

func Secrets(state *RenderState, listeners *redpanda.Listeners) []*corev1.Secret {
	var secrets []*corev1.Secret
	secrets = append(secrets, SecretSTSLifecycle(state, listeners))
	if saslUsers := SecretSASLUsers(state); saslUsers != nil {
		secrets = append(secrets, saslUsers)
	}
	// ordinalOffset tracks the global broker ordinal where each pool begins, in
	// the same main-then-pools order as [gatewayPodNames]. The configurator
	// renders advertised addresses with a pool-local ordinal, so it needs this
	// offset to recover the global ordinal that Gateway TLSRoutes/services use.
	secrets = append(secrets, SecretConfigurator(state, listeners, Pool{Statefulset: state.Values.Statefulset}, 0))
	if fsValidator := SecretFSValidator(state, Pool{Statefulset: state.Values.Statefulset}); fsValidator != nil {
		secrets = append(secrets, fsValidator)
	}
	ordinalOffset := int(state.Values.Statefulset.Replicas)
	for _, set := range state.Pools {
		secrets = append(secrets, SecretConfigurator(state, listeners, set, ordinalOffset))
		if fsValidator := SecretFSValidator(state, set); fsValidator != nil {
			secrets = append(secrets, fsValidator)
		}
		ordinalOffset = ordinalOffset + int(set.Statefulset.Replicas)
	}
	if bootstrapUser := SecretBootstrapUser(state); bootstrapUser != nil {
		secrets = append(secrets, bootstrapUser)
	}
	return secrets
}

func SecretSTSLifecycle(state *RenderState, listeners *redpanda.Listeners) *corev1.Secret {
	replicas := state.Values.Statefulset.Replicas
	for _, set := range state.Pools {
		replicas = replicas + set.Statefulset.Replicas
	}

	adminCurlFlags := listeners.Admin().CurlFlags()
	drain := replicas > 2 && !helmette.Dig(state.Values.Config.Node, false, "recovery_mode_enabled").(bool)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-sts-lifecycle", Fullname(state)),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"common.sh": redpanda.LifecycleCommonSh(
				adminInternalURL(state),
				adminCurlFlags,
				fmt.Sprintf("${SERVICE_NAME}.%s", InternalDomain(state)),
			),
			"postStart.sh": redpanda.LifecyclePostStartSh(adminCurlFlags),
			"preStop.sh":   redpanda.LifecyclePreStopSh(adminCurlFlags, drain),
		},
	}

	return secret
}

func SecretSASLUsers(state *RenderState) *corev1.Secret {
	if state.Values.Auth.SASL.SecretRef != "" && state.Values.Auth.SASL.Enabled && len(state.Values.Auth.SASL.Users) > 0 {
		secret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        state.Values.Auth.SASL.SecretRef,
				Namespace:   state.Release.Namespace,
				Labels:      FullLabels(state),
				Annotations: FullAnnotations(state),
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{},
		}
		usersTxt := []string{}

		defaultMechanism := state.Values.Auth.SASL.GetMechanism()

		// Working around lack of support for += or strings.Join at the moment
		for _, user := range state.Values.Auth.SASL.Users {
			mechanism := ptr.Deref(user.Mechanism, defaultMechanism)
			usersTxt = append(usersTxt, fmt.Sprintf("%s:%s:%s", user.Name, user.Password, mechanism))
		}
		secret.StringData["users.txt"] = helmette.Join("\n", usersTxt)
		return secret
	} else if state.Values.Auth.SASL.Enabled && state.Values.Auth.SASL.SecretRef == "" {
		panic("auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true")
	} else {
		// XXX no secret generated when enabled, we have a secret ref, but we have no users
		return nil
	}
}

func SecretBootstrapUser(state *RenderState) *corev1.Secret {
	if !state.Values.Auth.SASL.Enabled || state.Values.Auth.SASL.BootstrapUser.SecretKeyRef != nil {
		return nil
	}

	secretName := fmt.Sprintf("%s-bootstrap-user", Fullname(state))

	if state.BootstrapUserSecret != nil {
		return state.BootstrapUserSecret
	}

	password := helmette.RandAlphaNum(32)

	userPassword := state.Values.Auth.SASL.BootstrapUser.Password
	if userPassword != nil {
		password = *userPassword
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Immutable: ptr.To(true),
		Type:      corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": password,
		},
	}
}

func SecretFSValidator(state *RenderState, pool Pool) *corev1.Secret {
	if !pool.Statefulset.InitContainers.FSValidator.Enabled {
		return nil
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%.49s-fs-validator", fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{},
	}

	secret.StringData["fsValidator.sh"] = redpanda.FSValidatorSh()
	return secret
}

func SecretConfigurator(state *RenderState, listeners *redpanda.Listeners, pool Pool, ordinalOffset int) *corev1.Secret {
	configuratorSh := redpanda.ConfiguratorPrologueSh()

	kafkaSnippet := secretConfiguratorAdvertisedConfig(state, listeners.Kafka(), pool.Statefulset, ordinalOffset)
	configuratorSh = append(configuratorSh, kafkaSnippet...)

	httpSnippet := secretConfiguratorAdvertisedConfig(state, listeners.HTTP(), pool.Statefulset, ordinalOffset)
	configuratorSh = append(configuratorSh, httpSnippet...)

	if state.Values.RackAwareness.Enabled {
		configuratorSh = append(configuratorSh, redpanda.ConfiguratorRackAwarenessSh(state.Values.RackAwareness.NodeAnnotation)...)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%.51s-configurator", fmt.Sprintf("%s%s", Fullname(state), pool.Suffix())),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"configurator.sh": strings.Join(configuratorSh, "\n"),
		},
	}
}

// secretConfiguratorAdvertisedConfig emits the configurator snippet setting one
// API's advertised addresses: the internal one at index 0, then a per-replica
// bash array per external listener, indexed by $POD_ORDINAL.
func secretConfiguratorAdvertisedConfig(state *RenderState, api *redpanda.API, sts Statefulset, ordinalOffset int) []string {
	internalAdvertiseAddress := fmt.Sprintf("%s.%s", "${SERVICE_NAME}", InternalDomain(state))

	inCluster := api.InCluster()

	var snippet []string

	snippet = append(snippet,
		``,
		fmt.Sprintf(`LISTENER=%s`, helmette.Quote(helmette.ToJSON(map[string]any{
			"name":    redpanda.InternalListenerName,
			"address": internalAdvertiseAddress,
			"port":    inCluster.Port,
		}))),
		fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.%s[0] "$LISTENER"`,
			api.Kind.AdvertisedConfigSection(),
			api.Kind.AdvertisedConfigKey(),
		),
	)

	arrayName := helmette.Upper(string(api.Kind))

	// NB: ungated on exposure. A listener with no Service is still advertised;
	// values.yaml says the user may create that Service themselves.
	externalCounter := 0
	for _, listener := range api.External() {
		externalCounter = externalCounter + 1

		snippet = append(snippet,
			``,
			fmt.Sprintf(`ADVERTISED_%s_ADDRESSES=()`, arrayName),
		)

		// TODO: this looks quite broken just based on the fact that if replicas > addresses
		for _, replicaIndex := range helmette.Until(int(sts.Replicas)) {
			host := advertisedHostJSON(
				state,
				listener.Name,
				listener.AdvertisedPort(int32(replicaIndex)),
				replicaIndex,
				ordinalOffset+replicaIndex,
				listener.Gateway,
			)
			// XXX: the original code used the stringified `host` value as a template
			// for re-expansion; however it was impossible to make this work usefully,
			/// even with the original yaml template.
			address := helmette.ToJSON(host)

			prefixTemplate := listener.PrefixTemplate
			if prefixTemplate == "" {
				// Required because the values might not specify this, it'll ensur we see "" if it's missing.
				prefixTemplate = helmette.Default("", state.Values.External.PrefixTemplate)
			}

			snippet = append(snippet,
				``,
				fmt.Sprintf(`PREFIX_TEMPLATE=%s`, helmette.Quote(prefixTemplate)),
				fmt.Sprintf(`ADVERTISED_%s_ADDRESSES+=(%s)`, arrayName, helmette.Quote(address)),
			)
		}

		snippet = append(snippet,
			``,
			fmt.Sprintf(`rpk redpanda config --config "$CONFIG" set %s.%s[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"`,
				api.Kind.AdvertisedConfigSection(),
				api.Kind.AdvertisedConfigKey(),
				externalCounter,
				arrayName,
			),
		)
	}

	return snippet
}

// The following from _helpers.tpm

func externalAdvertiseAddress(state *RenderState) string {
	eaa := "${SERVICE_NAME}"
	externalDomainTemplate := ptr.Deref(state.Values.External.Domain, "")
	expanded := helmette.Tpl(state.Dot, externalDomainTemplate, state.Dot)
	if !helmette.Empty(expanded) {
		eaa = fmt.Sprintf("%s.%s", "${SERVICE_NAME}", expanded)
	}
	return eaa
}

// was advertised-host
func advertisedHostJSON(state *RenderState, name string, port int32, replicaIndex int, globalOrdinal int, gateway *redpanda.GatewayRoute) map[string]any {
	// Gateway API mode: advertise the TLSRoute SNI hostname and the
	// gateway's advertised port (default 443) rather than a NodePort/LB address.
	// Only applies to listeners that opted into gateway mode.
	//
	// NB: gateway mode keys off globalOrdinal, not the pool-local replicaIndex.
	// The configurator renders per node pool with a StatefulSet-local ordinal,
	// but TLSRoutes/services are named/hosted by the global pod-list index, so a
	// pool broker must advertise the host at its global ordinal to match them.
	if state.Values.External.IsGatewayEnabled() && gateway != nil {
		return advertisedHostJSONGateway(state, name, globalOrdinal, gateway)
	}

	hostMap := map[string]any{
		"name":    name,
		"address": externalAdvertiseAddress(state),
		"port":    port,
	}
	if len(state.Values.External.Addresses) > 0 {
		address := ""
		if len(state.Values.External.Addresses) > 1 {
			address = state.Values.External.Addresses[replicaIndex]
		} else {
			address = state.Values.External.Addresses[0]
		}
		if domain := ptr.Deref(state.Values.External.Domain, ""); domain != "" {
			hostMap = map[string]any{
				"name":    name,
				"address": fmt.Sprintf("%s.%s", address, helmette.Tpl(state.Dot, domain, state.Dot)),
				"port":    port,
			}
		} else {
			hostMap = map[string]any{
				"name":    name,
				"address": address,
				"port":    port,
			}
		}
	}
	return hostMap
}

// advertisedHostJSONGateway builds the advertised host entry for Gateway API
// mode. The address is the per-broker SNI hostname (from HostTemplate) and the
// port is the gateway's advertised port (default 443). globalOrdinal is the
// broker's index into [gatewayPodNames] (the same index used to name/host its
// TLSRoute and per-broker service), so the advertised address matches the route
// that carries it.
func advertisedHostJSONGateway(state *RenderState, name string, globalOrdinal int, gateway *redpanda.GatewayRoute) map[string]any {
	gw := state.Values.External.Gateway
	port := gw.GatewayAdvertisedPort()

	// NB: empty when the listener set no hostTemplate, leaving the bootstrap
	// host as every broker's address.
	address := gateway.Host
	if globalOrdinal < len(gateway.BrokerHosts) {
		address = gateway.BrokerHosts[globalOrdinal]
	}

	return map[string]any{
		"name":    name,
		"address": address,
		"port":    port,
	}
}

// adminInternalHTTPProtocol was admin-http-protocol
func adminInternalHTTPProtocol(state *RenderState) string {
	if state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) {
		return "https"
	}
	return "http"
}

// Additional helpers

func adminInternalURL(state *RenderState) string {
	// NB: SERVICE_NAME here actually refers to the podname via the downward
	// API.
	return fmt.Sprintf("%s://%s.%s:%d",
		adminInternalHTTPProtocol(state),
		`${SERVICE_NAME}`,
		strings.TrimSuffix(InternalDomain(state), "."),
		state.Values.Listeners.Admin.Port,
	)
}
