// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_configmap.go.tpl
package redpanda

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"
)

func ConfigMaps(state *RenderState) []*corev1.ConfigMap {
	cms := []*corev1.ConfigMap{RedpandaConfigMap(state), RPKProfile(state)}
	return cms
}

func RedpandaConfigMap(state *RenderState) *corev1.ConfigMap {
	bootstrap, fixups := BootstrapFile(state)
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      Fullname(state),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Data: map[string]string{
			clusterconfiguration.BootstrapTemplateFile:    bootstrap,
			clusterconfiguration.BootstrapFixupFile:       fixups,
			clusterconfiguration.RedpandaYamlTemplateFile: RedpandaConfigFile(state, true /* includeSeedServer */),
		},
	}
}

// BootstrapFile returns contents of `.bootstrap.yaml`. Keys that may be set
// via environment variables (such as tiered storage secrets) will have
// placeholders expressed using fixups for $ENVVARNAME. An init container is responsible
// for expanding said placeholders.
//
// Convention is to name envvars
// $REDPANDA_SCREAMING_CASE_CLUSTER_PROPERTY_NAME. For example,
// cloud_storage_secret_key would be $REDPANDA_CLOUD_STORAGE_SECRET_KEY.
//
// `.bootstrap.yaml` is templated and then read by both the redpanda container
// and the post install/upgrade job.
func BootstrapFile(state *RenderState) (string, string) {
	template, fixups := BootstrapContents(state)
	fixupStr := helmette.ToJSON(fixups)
	if len(fixups) == 0 {
		fixupStr = `[]`
	}
	return helmette.ToJSON(template), fixupStr
}

func BootstrapContents(state *RenderState) (map[string]string, []clusterconfiguration.Fixup) {
	// Accumulate values and fixups
	fixups := []clusterconfiguration.Fixup{}

	bootstrap := map[string]any{
		"kafka_enable_authorization": state.Values.Auth.IsSASLEnabled(),
		"enable_sasl":                state.Values.Auth.IsSASLEnabled(),
		"enable_rack_awareness":      state.Values.RackAwareness.Enabled,
		"storage_min_free_bytes":     state.Values.Storage.StorageMinFreeBytes(),
	}

	bootstrap = helmette.Merge(bootstrap, state.Values.AuditLogging.Translate(state, state.Values.Auth.IsSASLEnabled()))
	bootstrap = helmette.Merge(bootstrap, state.Values.Logging.Translate())
	bootstrap = helmette.Merge(bootstrap, state.Values.Config.Tunable.Translate())
	bootstrap = helmette.Merge(bootstrap, state.Values.Config.Cluster.Translate())
	bootstrap = helmette.Merge(bootstrap, state.Values.Auth.Translate(state.Values.Auth.IsSASLEnabled()))
	attrs, fixes := state.Values.Storage.GetTieredStorageConfig().Translate(&state.Values.Storage.Tiered.CredentialsSecretRef)
	bootstrap = helmette.Merge(bootstrap, attrs)
	fixups = append(fixups, fixes...)

	// If default_topic_replications is not set and we have at least 3 Brokers,
	// upgrade from redpanda's default of 1 to 3 so, when possible, topics are
	// HA by default.
	// See also:
	// - https://github.com/redpanda-data/helm-charts/issues/583
	// - https://github.com/redpanda-data/helm-charts/issues/1501
	if _, ok := state.Values.Config.Cluster["default_topic_replications"]; !ok && state.Values.Statefulset.Replicas >= 3 {
		bootstrap["default_topic_replications"] = 3
	}

	if _, ok := state.Values.Config.Cluster["storage_min_free_bytes"]; !ok {
		bootstrap["storage_min_free_bytes"] = state.Values.Storage.StorageMinFreeBytes()
	}

	template := map[string]string{}
	for k, v := range bootstrap {
		template[k] = helmette.ToJSON(v)
	}

	// Fold in any extraClusterConfiguration values
	extra, fixes, _ := state.Values.Config.ExtraClusterConfiguration.Translate()
	template = helmette.Merge(template, extra)
	fixups = append(fixups, fixes...)

	return template, fixups
}

func RedpandaConfigFile(state *RenderState, includeNonHashableItems bool) string {
	redpanda := map[string]any{
		"empty_seed_starts_cluster": false,
	}

	if includeNonHashableItems {
		redpanda["seed_servers"] = state.Values.Listeners.CreateSeedServers(state.Values.Statefulset.Replicas, Fullname(state), InternalDomain(state))
	}

	redpanda = helmette.Merge(redpanda, state.Values.Config.Node.Translate())

	configureListeners(redpanda, state)

	redpandaYaml := map[string]any{
		"redpanda":        redpanda,
		"schema_registry": schemaRegistry(state),
		"pandaproxy":      pandaProxyListener(state),
		"config_file":     "/etc/redpanda/redpanda.yaml",
	}

	if includeNonHashableItems {
		redpandaYaml["rpk"] = rpkNodeConfig(state)
		redpandaYaml["pandaproxy_client"] = kafkaClient(state)
		redpandaYaml["schema_registry_client"] = kafkaClient(state)
		if RedpandaAtLeast_23_3_0(state) && state.Values.AuditLogging.Enabled && state.Values.Auth.IsSASLEnabled() {
			redpandaYaml["audit_log_client"] = kafkaClient(state)
		}
	}

	return helmette.ToYaml(redpandaYaml)
}

// RPKProfile returns a [corev1.ConfigMap] for aiding users in connecting to
// the external listeners of their redpanda cluster.
// It is meant for external consumption via NOTES.txt and is not used within
// this chart.
func RPKProfile(state *RenderState) *corev1.ConfigMap {
	if !state.Values.External.Enabled {
		return nil
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-rpk", Fullname(state)),
			Namespace: state.Release.Namespace,
			Labels:    FullLabels(state),
		},
		Data: map[string]string{
			"profile": helmette.ToYaml(rpkProfile(state)),
		},
	}
}

// rpkProfile generates an RPK Profile for connecting to external listeners.
// It is intended to be used by the end user via a prompt in NOTES.txt.
func rpkProfile(state *RenderState) map[string]any {
	brokerList := []string{}
	for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
		brokerList = append(brokerList, fmt.Sprintf("%s:%d", advertisedHost(state, i), int(advertisedKafkaPort(state, i))))
	}

	adminAdvertisedList := []string{}
	for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
		adminAdvertisedList = append(adminAdvertisedList, fmt.Sprintf("%s:%d", advertisedHost(state, i), int(advertisedAdminPort(state, i))))
	}

	schemaAdvertisedList := []string{}
	for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
		schemaAdvertisedList = append(schemaAdvertisedList, fmt.Sprintf("%s:%d", advertisedHost(state, i), int(advertisedSchemaPort(state, i))))
	}

	kafkaTLS := rpkKafkaClientTLSConfiguration(state)
	if _, ok := kafkaTLS["ca_file"]; ok {
		kafkaTLS["ca_file"] = "ca.crt"
	}

	adminTLS := rpkAdminAPIClientTLSConfiguration(state)
	if _, ok := adminTLS["ca_file"]; ok {
		adminTLS["ca_file"] = "ca.crt"
	}

	schemaTLS := rpkSchemaRegistryClientTLSConfiguration(state)
	if _, ok := schemaTLS["ca_file"]; ok {
		schemaTLS["ca_file"] = "ca.crt"
	}

	ka := map[string]any{
		"brokers": brokerList,
		"tls":     nil,
	}

	if len(kafkaTLS) > 0 {
		ka["tls"] = kafkaTLS
	}

	aa := map[string]any{
		"addresses": adminAdvertisedList,
		"tls":       nil,
	}

	if len(adminTLS) > 0 {
		aa["tls"] = adminTLS
	}

	sa := map[string]any{
		"addresses": schemaAdvertisedList,
		"tls":       nil,
	}

	if len(schemaTLS) > 0 {
		sa["tls"] = schemaTLS
	}

	result := map[string]any{
		"name":            getFirstExternalKafkaListener(state),
		"kafka_api":       ka,
		"admin_api":       aa,
		"schema_registry": sa,
	}

	return result
}

func advertisedKafkaPort(state *RenderState, i int32) int {
	externalKafkaListenerName := getFirstExternalKafkaListener(state)

	listener := state.Values.Listeners.Kafka.External[externalKafkaListenerName]

	port := int(state.Values.Listeners.Kafka.Port)

	if int(listener.Port) > int(1) {
		port = int(listener.Port)
	}

	if len(listener.AdvertisedPorts) > 1 {
		port = int(listener.AdvertisedPorts[i])
	} else if len(listener.AdvertisedPorts) == 1 {
		port = int(listener.AdvertisedPorts[0])
	}

	return port
}

func advertisedAdminPort(state *RenderState, i int32) int {
	keys := helmette.Keys(state.Values.Listeners.Admin.External)

	helmette.SortAlpha(keys)

	externalAdminListenerName := helmette.First(keys)

	listener := state.Values.Listeners.Admin.External[externalAdminListenerName.(string)]

	port := int(state.Values.Listeners.Admin.Port)

	if int(listener.Port) > 1 {
		port = int(listener.Port)
	}

	if len(listener.AdvertisedPorts) > 1 {
		port = int(listener.AdvertisedPorts[i])
	} else if len(listener.AdvertisedPorts) == 1 {
		port = int(listener.AdvertisedPorts[0])
	}

	return port
}

func advertisedSchemaPort(state *RenderState, i int32) int {
	keys := helmette.Keys(state.Values.Listeners.SchemaRegistry.External)

	helmette.SortAlpha(keys)

	externalSchemaListenerName := helmette.First(keys)

	listener := state.Values.Listeners.SchemaRegistry.External[externalSchemaListenerName.(string)]

	port := int(state.Values.Listeners.SchemaRegistry.Port)

	if int(listener.Port) > 1 {
		port = int(listener.Port)
	}

	if len(listener.AdvertisedPorts) > 1 {
		port = int(listener.AdvertisedPorts[i])
	} else if len(listener.AdvertisedPorts) == 1 {
		port = int(listener.AdvertisedPorts[0])
	}

	return port
}

func advertisedHost(state *RenderState, i int32) string {
	address := fmt.Sprintf("%s-%d", Fullname(state), int(i))
	if ptr.Deref(state.Values.External.Domain, "") != "" {
		address = fmt.Sprintf("%s.%s", address, helmette.Tpl(state.dot, *state.Values.External.Domain, state.dot))
	}

	if len(state.Values.External.Addresses) <= 0 {
		return address
	}

	if len(state.Values.External.Addresses) == 1 {
		address = state.Values.External.Addresses[0]
	} else {
		address = state.Values.External.Addresses[i]
	}

	if ptr.Deref(state.Values.External.Domain, "") != "" {
		address = fmt.Sprintf("%s.%s", address, helmette.Tpl(state.dot, *state.Values.External.Domain, state.dot))
	}

	return address
}

func getFirstExternalKafkaListener(state *RenderState) string {
	keys := helmette.Keys(state.Values.Listeners.Kafka.External)

	helmette.SortAlpha(keys)

	return helmette.First(keys).(string)
}

func BrokerList(state *RenderState, replicas int32, port int32) []string {
	var bl []string

	for i := int32(0); i < replicas; i++ {
		bl = append(bl, fmt.Sprintf("%s-%d.%s:%d", Fullname(state), i, InternalDomain(state), port))
	}

	return bl
}

// https://github.com/redpanda-data/redpanda/blob/817450a480f4f2cadf66de1adc301cfaf6ccde46/src/go/rpk/pkg/config/redpanda_yaml.go#L143
func rpkNodeConfig(state *RenderState) map[string]any {
	brokerList := BrokerList(state, state.Values.Statefulset.Replicas, state.Values.Listeners.Kafka.Port)

	var adminTLS map[string]any
	if tls := rpkAdminAPIClientTLSConfiguration(state); len(tls) > 0 {
		adminTLS = tls
	}

	var brokerTLS map[string]any
	if tls := rpkKafkaClientTLSConfiguration(state); len(tls) > 0 {
		brokerTLS = tls
	}

	var schemaRegistryTLS map[string]any
	if tls := rpkSchemaRegistryClientTLSConfiguration(state); len(tls) > 0 {
		schemaRegistryTLS = tls
	}

	lockMemory, overprovisioned, flags := RedpandaAdditionalStartFlags(&state.Values)

	result := map[string]any{
		"additional_start_flags": flags,
		"enable_memory_locking":  lockMemory,
		"overprovisioned":        overprovisioned,
		"kafka_api": map[string]any{
			"brokers": brokerList,
			"tls":     brokerTLS,
		},
		"admin_api": map[string]any{
			"addresses": state.Values.Listeners.AdminList(state.Values.Statefulset.Replicas, Fullname(state), InternalDomain(state)),
			"tls":       adminTLS,
		},
		"schema_registry": map[string]any{
			"addresses": state.Values.Listeners.SchemaRegistryList(state.Values.Statefulset.Replicas, Fullname(state), InternalDomain(state)),
			"tls":       schemaRegistryTLS,
		},
	}

	result = helmette.Merge(result, state.Values.Tuning.Translate())
	result = helmette.Merge(result, state.Values.Config.CreateRPKConfiguration())

	return result
}

// rpkKafkaClientTLSConfiguration returns a value suitable for use as RPK's
// "TLS" type.
// https://github.com/redpanda-data/redpanda/blob/817450a480f4f2cadf66de1adc301cfaf6ccde46/src/go/rpk/pkg/config/redpanda_yaml.go#L178
func rpkKafkaClientTLSConfiguration(state *RenderState) map[string]any {
	tls := state.Values.Listeners.Kafka.TLS

	if !tls.IsEnabled(&state.Values.TLS) {
		return map[string]any{}
	}

	result := map[string]any{
		"ca_file": tls.ServerCAPath(&state.Values.TLS),
	}

	if tls.RequireClientAuth {
		result["cert_file"] = fmt.Sprintf("%s/%s-client/tls.crt", certificateMountPoint, Fullname(state))
		result["key_file"] = fmt.Sprintf("%s/%s-client/tls.key", certificateMountPoint, Fullname(state))
	}

	return result
}

// rpkAdminAPIClientTLSConfiguration returns a value suitable for use as RPK's
// "TLS" type.
// https://github.com/redpanda-data/redpanda/blob/817450a480f4f2cadf66de1adc301cfaf6ccde46/src/go/rpk/pkg/config/redpanda_yaml.go#L184
func rpkAdminAPIClientTLSConfiguration(state *RenderState) map[string]any {
	tls := state.Values.Listeners.Admin.TLS

	if !tls.IsEnabled(&state.Values.TLS) {
		return map[string]any{}
	}

	result := map[string]any{
		"ca_file": tls.ServerCAPath(&state.Values.TLS),
	}

	if tls.RequireClientAuth {
		result["cert_file"] = fmt.Sprintf("%s/%s-client/tls.crt", certificateMountPoint, Fullname(state))
		result["key_file"] = fmt.Sprintf("%s/%s-client/tls.key", certificateMountPoint, Fullname(state))
	}

	return result
}

// rpkSchemaRegistryClientTLSConfiguration returns a value suitable for use as RPK's
// "TLS" type.
// https://github.com/redpanda-data/redpanda/blob/817450a480f4f2cadf66de1adc301cfaf6ccde46/src/go/rpk/pkg/config/redpanda_yaml.go#L184
func rpkSchemaRegistryClientTLSConfiguration(state *RenderState) map[string]any {
	tls := state.Values.Listeners.SchemaRegistry.TLS

	if !tls.IsEnabled(&state.Values.TLS) {
		return map[string]any{}
	}

	result := map[string]any{
		"ca_file": tls.ServerCAPath(&state.Values.TLS),
	}

	if tls.RequireClientAuth {
		result["cert_file"] = fmt.Sprintf("%s/%s-client/tls.crt", certificateMountPoint, Fullname(state))
		result["key_file"] = fmt.Sprintf("%s/%s-client/tls.key", certificateMountPoint, Fullname(state))
	}

	return result
}

// kafkaClient returns the configuration for internal components of redpanda to
// connect to its own Kafka API. This is distinct from RPK's configuration for
// Kafka API interactions.
func kafkaClient(state *RenderState) map[string]any {
	brokerList := []map[string]any{}
	for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
		brokerList = append(brokerList, map[string]any{
			"address": fmt.Sprintf("%s-%d.%s", Fullname(state), i, InternalDomain(state)),
			"port":    state.Values.Listeners.Kafka.Port,
		})
	}

	kafkaTLS := state.Values.Listeners.Kafka.TLS

	var brokerTLS map[string]any
	if state.Values.Listeners.Kafka.TLS.IsEnabled(&state.Values.TLS) {
		brokerTLS = map[string]any{
			"enabled":             true,
			"require_client_auth": kafkaTLS.RequireClientAuth,
			// NB: truststore_file here is synonymous with ca_file in the RPK
			// configuration. The difference being that redpanda does NOT read
			// the ca_file key.
			"truststore_file": kafkaTLS.ServerCAPath(&state.Values.TLS),
		}

		if kafkaTLS.RequireClientAuth {
			brokerTLS["cert_file"] = fmt.Sprintf("%s/%s-client/tls.crt", certificateMountPoint, Fullname(state))
			brokerTLS["key_file"] = fmt.Sprintf("%s/%s-client/tls.key", certificateMountPoint, Fullname(state))
		}

	}

	cfg := map[string]any{
		"brokers": brokerList,
	}
	if len(brokerTLS) > 0 {
		cfg["broker_tls"] = brokerTLS
	}

	return cfg
}

func configureListeners(redpanda map[string]any, state *RenderState) {
	var defaultKafkaAuth *KafkaAuthenticationMethod
	if state.Values.Auth.SASL.Enabled {
		defaultKafkaAuth = ptr.To(SASLKafkaAuthenticationMethod)
	}

	redpanda["admin"] = state.Values.Listeners.Admin.Listeners(nil /* No auth on admin API */)
	redpanda["kafka_api"] = state.Values.Listeners.Kafka.Listeners(defaultKafkaAuth)
	redpanda["rpc_server"] = rpcListeners(state)

	// Backwards compatibility layer, if any of the *_tls keys are an empty
	// slice, they should instead be nil.

	redpanda["admin_api_tls"] = nil
	if tls := state.Values.Listeners.Admin.ListenersTLS(&state.Values.TLS); len(tls) > 0 {
		redpanda["admin_api_tls"] = tls
	}

	redpanda["kafka_api_tls"] = nil
	if tls := state.Values.Listeners.Kafka.ListenersTLS(&state.Values.TLS); len(tls) > 0 {
		redpanda["kafka_api_tls"] = tls
	}

	// With the exception of rpc_server_tls, it should just not be specified.
	if tls := rpcListenersTLS(state); len(tls) > 0 {
		redpanda["rpc_server_tls"] = tls
	}
}

func pandaProxyListener(state *RenderState) map[string]any {
	pandaProxy := map[string]any{}

	var pandaProxyAuth *HTTPAuthenticationMethod
	if state.Values.Auth.IsSASLEnabled() {
		pandaProxyAuth = ptr.To(BasicHTTPAuthenticationMethod)
	}

	pandaProxy["pandaproxy_api"] = state.Values.Listeners.HTTP.Listeners(pandaProxyAuth)
	pandaProxy["pandaproxy_api_tls"] = nil
	if tls := state.Values.Listeners.HTTP.ListenersTLS(&state.Values.TLS); len(tls) > 0 {
		pandaProxy["pandaproxy_api_tls"] = tls
	}
	return pandaProxy
}

func schemaRegistry(state *RenderState) map[string]any {
	schemaReg := map[string]any{}
	schemaReg["schema_registry_api"] = state.Values.Listeners.SchemaRegistry.Listeners(nil /* No auth on admin API */)
	schemaReg["schema_registry_api_tls"] = nil
	if tls := state.Values.Listeners.SchemaRegistry.ListenersTLS(&state.Values.TLS); len(tls) > 0 {
		schemaReg["schema_registry_api_tls"] = tls
	}
	return schemaReg
}

func rpcListenersTLS(state *RenderState) map[string]any {
	r := state.Values.Listeners.RPC

	if !(RedpandaAtLeast_22_2_atleast_22_2_10(state) ||
		RedpandaAtLeast_22_3_atleast_22_3_13(state) ||
		RedpandaAtLeast_23_1_2(state)) && (r.TLS.Enabled == nil && state.Values.TLS.Enabled || ptr.Deref(r.TLS.Enabled, false)) {
		panic(fmt.Sprintf("Redpanda version v%s does not support TLS on the RPC port. Please upgrade. See technical service bulletin 2023-01.", helmette.TrimPrefix("v", Tag(state))))
	}

	if !r.TLS.IsEnabled(&state.Values.TLS) {
		return map[string]any{}
	}

	certName := r.TLS.Cert

	return map[string]any{
		"enabled":             true,
		"cert_file":           fmt.Sprintf("%s/%s/tls.crt", certificateMountPoint, certName),
		"key_file":            fmt.Sprintf("%s/%s/tls.key", certificateMountPoint, certName),
		"require_client_auth": r.TLS.RequireClientAuth,
		"truststore_file":     r.TLS.TrustStoreFilePath(&state.Values.TLS),
	}
}

func rpcListeners(state *RenderState) map[string]any {
	return map[string]any{
		"address": "0.0.0.0",
		"port":    state.Values.Listeners.RPC.Port,
	}
}

// First parameter defaultTLSEnabled must come from `state.Values.tls.enabled`.
func createInternalListenerTLSCfg(tls *TLS, internal InternalTLS) map[string]any {
	if !internal.IsEnabled(tls) {
		return map[string]any{}
	}

	return map[string]any{
		"name":                "internal",
		"enabled":             true,
		"cert_file":           fmt.Sprintf("%s/%s/tls.crt", certificateMountPoint, internal.Cert),
		"key_file":            fmt.Sprintf("%s/%s/tls.key", certificateMountPoint, internal.Cert),
		"require_client_auth": internal.RequireClientAuth,
		"truststore_file":     internal.TrustStoreFilePath(tls),
	}
}

// RedpandaAdditionalStartFlags returns a string slice of flags suitable for use
// as `additional_start_flags`. User provided flags will override any of those
// set by default.
func RedpandaAdditionalStartFlags(values *Values) (bool, bool, []string) {
	// All `additional_start_flags` that are set by the chart.
	flags := values.Resources.GetRedpandaFlags()
	flags["--default-log-level"] = values.Logging.LogLevel

	// Unclear why this is done aside from historical reasons.
	// Legacy comment: If in developer_mode, don't set reserve-memory.
	if values.Config.Node["developer_mode"] == true {
		delete(flags, "--reserve-memory")
	}

	for key, value := range helmette.SortedMap(ParseCLIArgs(values.Statefulset.AdditionalRedpandaCmdFlags)) {
		flags[key] = value
	}

	enabledOptions := map[string]bool{
		"true": true,
		"1":    true,
		"":     true,
	}

	// Due to a buglet in rpk, we need to set lock-memory and overprovisioned
	// via their fields in redpanda.yaml instead of additional_start_flags.
	// https://github.com/redpanda-data/helm-charts/pull/1622#issuecomment-2577922409
	lockMemory := false
	if value, ok := flags["--lock-memory"]; ok {
		lockMemory = enabledOptions[value]
		delete(flags, "--lock-memory")
	}

	overprovisioned := false
	if value, ok := flags["--overprovisioned"]; ok {
		overprovisioned = enabledOptions[value]
		delete(flags, "--overprovisioned")
	}

	// Deterministically order out list and add in values supplied flags.
	keys := helmette.Keys(flags)
	keys = helmette.SortAlpha(keys)

	var rendered []string
	for _, key := range keys {
		value := flags[key]
		// Support flags that don't have values (`--overprovisioned`) by
		// letting them be specified as key: ""
		if value == "" {
			rendered = append(rendered, key)
		} else {
			rendered = append(rendered, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return lockMemory, overprovisioned, rendered
}
