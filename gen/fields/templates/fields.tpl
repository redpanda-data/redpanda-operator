// Copyright {{ year }} Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package {{ .Pkg }}

import (
	corev1 "k8s.io/api/core/v1"
)

// RedpandaClusterConfiguration represents where values for configuration can be pulled from.
// Properties may be specified directly in properties, which will always
// take precedence, however values will initially get pulled from any exteernal secrets
// Kubernetes secrets, or config maps, if specified, before finally merging in the inline
// values. The order of merging goes from most sensitive initially to least sensitive,
// so we pull and merge in this order: externalSecretRef -> secretKeyRef -> configMapKeyRef -> properties
//
// +structType=atomic
// +kubebuilder:validation:XValidation:message="at least one of properties, configMapKeyRef, secretKeyRef, or externalSecretRef must be set",rule="has(self.properties) || has(self.configMapKeyRef) || has(self.secretKeyRef) || has(self.externalSecretRef)"
type RedpandaClusterConfiguration struct {
  // The typed property values we will merge into the configuration.
	Properties *ConfigurationProperties `json:"properties,omitempty"`
	// If the value is supplied by a kubernetes object reference, coordinates are embedded here.
	// For target values, the string value fetched from the source will be treated as
	// a raw configuration file.
	ConfigMapKeyRef *corev1.ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
	// Should the value be contained in a k8s secret rather than configmap, we can refer
	// to it here.
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef,omitempty"`
	// If the value is supplied by an external source, coordinates are embedded here.
	// Note: like configMapKeyRef, and secretKeyRef, fetched values are considered raw configuration
  // files.
	ExternalSecretRefSelector *ExternalSecretKeySelector `json:"externalSecretRef,omitempty"`
}

{{- range $property := .Properties }}{{ if $property.IsEnum }}
{{ $property.EnumComment }}
type {{ $property.EnumName }} string

const (
  {{ $property.EnumValueDefinitions }}
)
{{- end }}{{ end }}

type ConfigurationProperties struct {
  {{- range $i, $property := .Properties }}
  {{ if eq $i 0 }}{{ $property.Comment }}{{ else }}

  {{ $property.Comment }}{{ end }}
  {{ $property.Name }} {{ $property.GoType true }} {{ $property.JSONTag }}
  {{- end }}

  // Deprecations/Aliases

  {{ range $property := .Properties -}}
  {{- range $i, $alias := $property.Aliases }}
  // Deprecated: `{{ jsonAlias $alias }}` has been deprecated, use `{{ jsonAlias $property.Name }}` instead
  // +optional
  Deprecated{{ goName $alias }} {{ $property.GoType true }} `json:"{{ jsonAlias $alias }},omitempty" property:"{{ $property.OriginalName }},alias:{{ $i }}"`
  {{- end }}
  {{- end }}
}

func (c *ConfigurationProperties) Equals(other *ConfigurationProperties) bool {
  if c == nil && other == nil {
    return true
  }
  if c == nil || other == nil {
    return false
  }

  {{- range $property := .Properties }}
  if !{{ $property.EqualityCheck }} {
    return false
  }
  {{- end }}

  {{ range $property := .Properties -}}
  {{- range $i, $alias := $property.Aliases }}
  if !primitiveEquals(c.Deprecated{{ goName $alias }}, other.Deprecated{{ goName $alias }}) {
    return false
  }
  {{- end }}
  {{- end }}

  return true
}

// TODO: ThroughputControlGroup docs
type ThroughputControlGroup struct {
  // +optional
  Name *string `json:"name,omitempty" property:"name"`

  // +optional
  ClientId *string `json:"clientId,omitempty" property:"client_id"`
}

func (t ThroughputControlGroup) Equals(other ThroughputControlGroup) bool {
  if !primitiveEquals(t.Name, other.Name) {
    return false
  }

  return primitiveEquals(t.ClientId, other.ClientId)
}

// TODO: SaslMechanismsOverride docs
type SaslMechanismsOverride struct {
  // +optional
  Listener *string `json:"listener,omitempty" property:"listener"`

  // +optional
  SaslMechanisms *[]string `json:"saslMechanisms,omitempty" property:"sasl_mechanisms"`
}

func (s SaslMechanismsOverride) Equals(other SaslMechanismsOverride) bool {
  if !primitiveEquals(s.Listener, other.Listener) {
    return false
  }

  return arrayEquals(s.SaslMechanisms, other.SaslMechanisms)
}

// +kubebuilder:validation:XValidation:message="leadersPreference must be either 'none' or a comma separated list starting with 'racks:'",rule="self == 'none'  || self.startsWith('racks:')"
type LeadersPreference string

func primitiveEquals[T ~string | bool | float64 | int64](a, b *T) bool {
  if a == nil && b == nil {
    return true
  }
  if a == nil || b == nil {
    return false
  }

  return *a == *b
}

func arrayEquals[T ~string | bool | float64 | int64](a, b *[]T) bool {
  if a == nil && b == nil {
    return true
  }
  if a == nil || b == nil {
    return false
  }

  for i := range *a {
    if (*a)[i] != (*b)[i] {
      return false
    }
  }

  return true
}

type equalityChecker[T any] interface {
  Equals(T) bool
}

func complexArrayEquals[T any, U equalityChecker[T]] (a *[]U, b *[]T) bool {
  if a == nil && b == nil {
    return true
  }
  if a == nil || b == nil {
    return false
  }

  for i := range *a {
    if !(*a)[i].Equals((*b)[i]) {
      return false
    }
  }

  return  true
}