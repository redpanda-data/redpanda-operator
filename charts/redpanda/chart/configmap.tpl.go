// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_configmap.go.tpl
package chart

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v25"
	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
	"github.com/redpanda-data/redpanda-operator/pkg/chartutil"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

func ConfigMaps(state *RenderState, listeners *redpanda.Listeners) []*corev1.ConfigMap {
	cms := []*corev1.ConfigMap{RedpandaConfigMap(state, listeners, Pool{Statefulset: state.Values.Statefulset})}

	for _, set := range state.Pools {
		cms = append(cms, RedpandaConfigMap(state, listeners, set))
	}

	return append(cms, RPKProfile(state, listeners))
}

func RedpandaConfigMap(state *RenderState, listeners *redpanda.Listeners, pool Pool) *corev1.ConfigMap {
	bootstrap, fixups := BootstrapFile(state, pool)
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s%s", Fullname(state), pool.Suffix()),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Data: map[string]string{
			clusterconfiguration.BootstrapTemplateFile:    bootstrap,
			clusterconfiguration.BootstrapFixupFile:       fixups,
			clusterconfiguration.RedpandaYamlTemplateFile: RedpandaConfigFile(state, listeners, true /* includeSeedServer */, pool),
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
func BootstrapFile(state *RenderState, pool Pool) (string, string) {
	template, fixups := BootstrapContents(state, pool)
	fixupStr := helmette.ToJSON(fixups)
	if len(fixups) == 0 {
		fixupStr = `[]`
	}
	return helmette.ToJSON(template), fixupStr
}

func BootstrapContents(state *RenderState, pool Pool) (map[string]string, []clusterconfiguration.Fixup) {
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
	// Tiered storage settings act as defaults and must not clobber values that
	// were explicitly set via config.cluster. We can't use helmette.Merge here
	// because it relies on mergo, which treats a boolean false (and other zero
	// values) as "empty" and would let the tiered storage default (e.g.
	// cloud_storage_enable_remote_read: true) overwrite an explicit false. See
	// K8S-882. Only fold in tiered storage keys that haven't already been set.
	for k, v := range attrs {
		if _, ok := bootstrap[k]; !ok {
			bootstrap[k] = v
		}
	}
	fixups = append(fixups, fixes...)

	// If default_topic_replications is not set and we have at least 3 Brokers,
	// upgrade from redpanda's default of 1 to 3 so, when possible, topics are
	// HA by default.
	// See also:
	// - https://github.com/redpanda-data/helm-charts/issues/583
	// - https://github.com/redpanda-data/helm-charts/issues/1501
	if _, ok := state.Values.Config.Cluster["default_topic_replications"]; !ok && pool.Statefulset.Replicas >= 3 {
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
	template = helmette.Merge(extra, template)
	fixups = append(fixups, fixes...)

	return template, fixups
}

func RedpandaConfigFile(state *RenderState, listeners *redpanda.Listeners, includeNonHashableItems bool, pool Pool) string {
	redpandaConfig := map[string]any{
		"empty_seed_starts_cluster": false,
	}

	if includeNonHashableItems {
		// NB: BrokerList returns fully qualified hosts, already ordered.
		rpcPort := listeners.RPC().InCluster().Port

		var seeds []map[string]any
		for _, host := range BrokerList(state, -1) {
			seeds = append(seeds, map[string]any{
				"host": map[string]any{
					"address": host,
					"port":    rpcPort,
				},
			})
		}

		redpandaConfig["seed_servers"] = seeds
	}

	redpandaConfig = helmette.Merge(redpandaConfig, state.Values.Config.Node.Translate())

	sections := listeners.ConfigSections()

	for _, key := range helmette.SortedKeys(sections["redpanda"]) {
		redpandaConfig[key] = sections["redpanda"][key]
	}

	redpandaYaml := map[string]any{
		"redpanda":        redpandaConfig,
		"schema_registry": sections["schema_registry"],
		"pandaproxy":      sections["pandaproxy"],
		"config_file":     "/etc/redpanda/redpanda.yaml",
	}

	if includeNonHashableItems {
		redpandaYaml["rpk"] = rpkNodeConfig(state, listeners, pool)
		redpandaYaml["pandaproxy_client"] = kafkaClient(state, listeners, "pandaproxy")
		redpandaYaml["schema_registry_client"] = kafkaClient(state, listeners, "schema_registry")
		if state.Values.AuditLogging.Enabled && state.Values.Auth.IsSASLEnabled() {
			redpandaYaml["audit_log_client"] = kafkaClient(state, listeners, "audit_log")
		}
	}

	return helmette.ToYaml(redpandaYaml)
}

// RPKProfile returns a [corev1.ConfigMap] for aiding users in connecting to
// the external listeners of their redpanda cluster.
// It is meant for external consumption via NOTES.txt and is not used within
// this chart.
func RPKProfile(state *RenderState, listeners *redpanda.Listeners) *corev1.ConfigMap {
	if !state.Values.External.Enabled {
		return nil
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-rpk", Fullname(state)),
			Namespace:   state.Release.Namespace,
			Labels:      FullLabels(state),
			Annotations: FullAnnotations(state),
		},
		Data: map[string]string{
			"profile": helmette.ToYaml(rpkProfile(state, listeners)),
		},
	}
}

// rpkProfile generates an RPK Profile for connecting to external listeners.
// It is intended to be used by the end user via a prompt in NOTES.txt.
func rpkProfile(state *RenderState, listeners *redpanda.Listeners) map[string]any {
	// NB: explicit empty slices. A zero-replica cluster renders these, where
	// `brokers: null` would be a user-visible change from `brokers: []`.
	brokerList := []string{}
	adminAdvertisedList := []string{}
	schemaAdvertisedList := []string{}

	for i := int32(0); i < state.Values.Statefulset.Replicas; i++ {
		host := advertisedHost(state, i)
		brokerList = append(brokerList, fmt.Sprintf("%s:%d", host, int(listeners.Kafka().ProfileAdvertisedPort(i))))
		adminAdvertisedList = append(adminAdvertisedList, fmt.Sprintf("%s:%d", host, int(listeners.Admin().ProfileAdvertisedPort(i))))
		schemaAdvertisedList = append(schemaAdvertisedList, fmt.Sprintf("%s:%d", host, int(listeners.SchemaRegistry().ProfileAdvertisedPort(i))))
	}

	// NB: the profile ships alongside the CA rather than referencing the
	// broker's mount, so every ca_file collapses to a bare filename.
	kafkaTLS := listeners.Kafka().RPKClientTLS()
	if len(kafkaTLS) > 0 {
		kafkaTLS["ca_file"] = "ca.crt"
	}

	adminTLS := listeners.Admin().RPKClientTLS()
	if len(adminTLS) > 0 {
		adminTLS["ca_file"] = "ca.crt"
	}

	schemaTLS := listeners.SchemaRegistry().RPKClientTLS()
	if len(schemaTLS) > 0 {
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

	// NB: the profile's name reads values, not [resolveAPIListeners], which drops
	// listeners Redpanda does not bind. A profile is still rendered when every
	// external listener is disabled, and `rpk profile create --from-profile`
	// rejects an empty name.
	var profileName string
	for name := range helmette.SortedMap(state.Values.Listeners.Kafka.External) {
		profileName = name
		break
	}

	result := map[string]any{
		"name":            profileName,
		"kafka_api":       ka,
		"admin_api":       aa,
		"schema_registry": sa,
	}

	return result
}

func advertisedHost(state *RenderState, i int32) string {
	address := fmt.Sprintf("%s-%d", Fullname(state), int(i))
	if ptr.Deref(state.Values.External.Domain, "") != "" {
		address = fmt.Sprintf("%s.%s", address, helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot))
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
		address = fmt.Sprintf("%s.%s", address, helmette.Tpl(state.Dot, *state.Values.External.Domain, state.Dot))
	}

	return address
}

// BrokerList returns the list of brokers referenced in every node pool in the RenderState.
// If the port specified is -1 then it is not appended onto the generated hostnames.
func BrokerList(state *RenderState, port int32) []string {
	bl := brokersFor(state, Pool{Statefulset: state.Values.Statefulset}, port)
	for _, set := range state.Pools {
		bl = append(bl, brokersFor(state, set, port)...)
	}

	return bl
}

func brokersFor(state *RenderState, pool Pool, port int32) []string {
	var bl []string

	for i := int32(0); i < pool.Statefulset.Replicas; i++ {
		if port == -1 {
			bl = append(bl, fmt.Sprintf("%s%s-%d.%s", Fullname(state), pool.Suffix(), i, InternalDomain(state)))
		} else {
			bl = append(bl, fmt.Sprintf("%s%s-%d.%s:%d", Fullname(state), pool.Suffix(), i, InternalDomain(state), port))
		}
	}
	return bl
}

// https://github.com/redpanda-data/redpanda/blob/817450a480f4f2cadf66de1adc301cfaf6ccde46/src/go/rpk/pkg/config/redpanda_yaml.go#L143
func rpkNodeConfig(state *RenderState, listeners *redpanda.Listeners, pool Pool) map[string]any {
	kafka := listeners.Kafka().InCluster()
	brokerList := BrokerList(state, kafka.Port)

	adminTLS := listeners.Admin().RPKClientTLS()
	brokerTLS := listeners.Kafka().RPKClientTLS()
	schemaRegistryTLS := listeners.SchemaRegistry().RPKClientTLS()

	lockMemory, overprovisioned, flags := RedpandaAdditionalStartFlags(&state.Values, pool)

	result := map[string]any{
		"additional_start_flags": flags,
		"enable_memory_locking":  lockMemory,
		"overprovisioned":        overprovisioned,
		"kafka_api": map[string]any{
			"brokers": brokerList,
			"tls":     brokerTLS,
		},
		"admin_api": map[string]any{
			"addresses": BrokerList(state, state.Values.Listeners.Admin.Port),
			"tls":       adminTLS,
		},
		"schema_registry": map[string]any{
			"addresses": BrokerList(state, state.Values.Listeners.SchemaRegistry.Port),
			"tls":       schemaRegistryTLS,
		},
	}

	result = helmette.Merge(result, state.Values.Tuning.Translate())
	result = helmette.Merge(result, state.Values.Config.CreateRPKConfiguration())

	// Merge is first-argument-wins, so merging the host tuner defaults
	// last gives them the LOWEST precedence: they only fill keys neither
	// values.tuning nor the user's config.rpk set explicitly. An operator
	// writing `config.rpk.tune_fstrim: false` keeps that opt-out even
	// with apply_host_tuners enabled.
	if state.Values.Tuning.ApplyHostTuners {
		result = helmette.Merge(result, redpanda.HostTunerDefaults())
	}

	return result
}

// kafkaClient returns the configuration for internal components of redpanda to
// connect to its own Kafka API. This is distinct from RPK's configuration for
// Kafka API interactions.
func kafkaClient(state *RenderState, listeners *redpanda.Listeners, clientType string) map[string]any {
	brokerList := []map[string]any{}

	useLocalhostKey := fmt.Sprintf("%s_client.use_localhost", clientType)
	useLocalhost := false

	val, ok := state.Values.Config.Node[useLocalhostKey]
	if ok {
		if helmette.KindIs("bool", val) {
			useLocalhost = val == true
		} else if helmette.KindIs("string", val) {
			strVal := val.(string)
			useLocalhost = strVal == "true" || strVal == "True" || strVal == "TRUE" || strVal == "1"
		}
	}

	if useLocalhost {
		brokerList = append(brokerList, map[string]any{
			"address": "localhost",
			"port":    state.Values.Listeners.Kafka.Port,
		})
	} else {
		for _, broker := range BrokerList(state, -1) {
			brokerList = append(brokerList, map[string]any{
				"address": broker,
				"port":    state.Values.Listeners.Kafka.Port,
			})
		}
	}

	brokerTLS := listeners.Kafka().BrokerClientTLS()

	cfg := map[string]any{
		"brokers": brokerList,
	}
	if len(brokerTLS) > 0 {
		cfg["broker_tls"] = brokerTLS
	}

	return cfg
}

// RedpandaAdditionalStartFlags returns a string slice of flags suitable for use
// as `additional_start_flags`. User provided flags will override any of those
// set by default.
func RedpandaAdditionalStartFlags(values *Values, pool Pool) (bool, bool, []string) {
	// All `additional_start_flags` that are set by the chart.
	flags := values.Resources.GetRedpandaFlags()
	flags["--default-log-level"] = values.Logging.LogLevel

	// Unclear why this is done aside from historical reasons.
	// Legacy comment: If in developer_mode, don't set reserve-memory.
	if values.Config.Node["developer_mode"] == true {
		delete(flags, "--reserve-memory")
	}

	for key, value := range helmette.SortedMap(chartutil.ParseFlags(pool.Statefulset.AdditionalRedpandaCmdFlags)) {
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
