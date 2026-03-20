// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/pkg/clusterconfiguration"
)

// clusterConfigurationToConfiguration translates ClusterConfiguration entries into
// bootstrap template values, fixups, and env vars. This mirrors the Helm chart's
// ClusterConfiguration.Translate() method.
//
// Each entry is one of four kinds:
//   - Repr: a literal string value → goes directly into the template map
//   - ConfigMapKeyRef / SecretKeyRef: a reference to a K8s resource → becomes an
//     env var (for envsubst) plus a CEL fixup expression
//   - ExternalSecretRefSelector: an ESO reference → becomes a CEL fixup only
func clusterConfigurationToConfiguration(cc redpandav1alpha2.ClusterConfiguration) (map[string]string, []clusterconfiguration.Fixup, []corev1.EnvVar) {
	if len(cc) == 0 {
		return nil, nil, nil
	}

	template := map[string]string{}
	var fixups []clusterconfiguration.Fixup
	var envVars []corev1.EnvVar

	for _, k := range sortedMapKeys(cc) {
		v := vectorizedv1alpha1.ClusterConfigValue(cc[k])

		switch {
		case v.Repr != nil:
			// Literal value — emit directly.
			template[k] = string(*v.Repr)

		case v.ConfigMapKeyRef != nil:
			// ConfigMap reference — inject via env var + fixup.
			env, fixup := envVarFixup(k, &corev1.EnvVarSource{ConfigMapKeyRef: v.ConfigMapKeyRef}, v.UseRawValue)
			envVars = append(envVars, env)
			fixups = append(fixups, fixup)

		case v.SecretKeyRef != nil:
			// Secret reference — inject via env var + fixup (same pattern as ConfigMap).
			env, fixup := envVarFixup(k, &corev1.EnvVarSource{SecretKeyRef: v.SecretKeyRef}, v.UseRawValue)
			envVars = append(envVars, env)
			fixups = append(fixups, fixup)

		case v.ExternalSecretRefSelector != nil:
			// External Secrets Operator reference — CEL fixup only, no env var.
			cel := fmt.Sprintf(`%s("%s")`, clusterconfiguration.CELExternalSecretRef, v.ExternalSecretRefSelector.Name)
			if !v.UseRawValue {
				cel = fmt.Sprintf(`%s(%s)`, clusterconfiguration.CELRepr, cel)
			}
			if ptr.Deref(v.ExternalSecretRefSelector.Optional, false) {
				cel = fmt.Sprintf(`%s(%s)`, clusterconfiguration.CELErrorToWarning, cel)
			}
			fixups = append(fixups, clusterconfiguration.Fixup{Field: k, CEL: cel})
		}
	}

	return template, fixups, envVars
}

// envVarFixup creates an env var + CEL fixup pair for a config key backed by
// a ConfigMap or Secret reference. When useRawValue is true the env var's string
// value is used as-is; otherwise it is wrapped in repr() for JSON encoding.
func envVarFixup(key string, source *corev1.EnvVarSource, useRawValue bool) (corev1.EnvVar, clusterconfiguration.Fixup) {
	envName := keyToEnvVar(key)

	cel := fmt.Sprintf(`%s("%s")`, clusterconfiguration.CELEnvString, envName)
	if !useRawValue {
		cel = fmt.Sprintf(`%s(%s)`, clusterconfiguration.CELRepr, cel)
	}

	return corev1.EnvVar{Name: envName, ValueFrom: source},
		clusterconfiguration.Fixup{Field: key, CEL: cel}
}

// keyToEnvVar converts a cluster config key to an environment variable name.
// e.g. "group.initial_rebalance_delay" → "REDPANDA_GROUP_INITIAL_REBALANCE_DELAY"
func keyToEnvVar(k string) string {
	return "REDPANDA_" + strings.ReplaceAll(strings.ToUpper(k), ".", "_")
}
