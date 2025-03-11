// Package clusterconfiguration holds types to track cluster configuration - the CRD declarations need to
// be transformed down into a representation of those values in a bootstrap template; we also supply an
// evaluator that can turn such a map of values into a map of raw concrete values.
package clusterconfiguration

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

type (
	ClusterConfigTemplateValue struct {
		// If the value is directly known, its yaml representation can be embedded here.
		Representation *vectorizedv1alpha1.YAMLRepresentation `json:"repr,omitempty"`
		// If the value is supplied by an external source, coordinates are embedded here.
		// Note: we interpret all fetched external secrets as string values and yam-encode them prior to embedding.
		// TODO: This decision needs finalising and documenting.
		ExternalSecretRef *string `json:"externalSecretRef,omitempty"`
		// Note: during the construction of a bootstrap template file, references to config objects above will be
		// rewritten to an EnvVarRef instead, and that variable added to the container environment via the usual k8s
		// envFrom mechanism. A preferred environment variable name may be specified here.
		EnvVarRef *string `json:"envVar,omitempty"`
	}
)

// ExpandForBootstrap passes over the input ClusterConfiguration and returns one with
// all ConfigMapKeyRef and SecretKeyRef entries turned into environment references,
// together with a list of env var definitions required to satisfy those.
func ExpandForBootstrap(cfg vectorizedv1alpha1.ClusterConfiguration) (map[string]ClusterConfigTemplateValue, []corev1.EnvVar, error) {
	expanded := make(map[string]ClusterConfigTemplateValue, len(cfg))
	ensureVar := func(k string) string {
		return "REDPANDA_" + strings.Replace(strings.ToUpper(k), ".", "_", -1)
	}
	var envs []corev1.EnvVar
	for k, v := range cfg {
		switch {
		case v.Representation != nil:
			expanded[k] = ClusterConfigTemplateValue{
				Representation: v.Representation,
			}
		case v.ConfigMapKeyRef != nil:
			envName := ensureVar(k)
			expanded[k] = ClusterConfigTemplateValue{
				EnvVarRef: &envName,
			}
			envs = append(envs, corev1.EnvVar{
				Name: envName,
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: v.ConfigMapKeyRef,
				},
			})
		case v.SecretKeyRef != nil:
			envName := ensureVar(k)
			expanded[k] = ClusterConfigTemplateValue{
				EnvVarRef: &envName,
			}
			envs = append(envs, corev1.EnvVar{
				Name: envName,
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: v.ConfigMapKeyRef,
				},
			})
		case v.ExternalSecretRef != nil:
			// We preserve this verbatim
			expanded[k] = ClusterConfigTemplateValue{
				ExternalSecretRef: v.ExternalSecretRef,
			}
		default:
			return nil, nil, fmt.Errorf("cluster config %q has no value: %#v", k, v)
		}
	}
	// Sort the env vars into alphabetical order
	sort.Slice(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
	return expanded, envs, nil
}

// ExpandForConfiguration takes a ClusterConfiguration and expands all references,
// both to k8s secrets and to external secrets.
// The result may be used directly against the AdminAPI of a running cluster, but
// should not be persisted to any k8s resource, since it may contain secrets.
// The schema is used to interpret any external representations.
// TODO: this should operate like ExpandEnv, with additional secret resolution.
// We'll probably want a Reader or something similar to pull out k8s values.
func ExpandForConfiguration(cfg vectorizedv1alpha1.ClusterConfiguration, schema rpadmin.ConfigSchema) (map[string]any, error) {
	return nil, fmt.Errorf("unimplemented")
}

// ExpandValueForTemplate is intended to run in an initContainer. All values are either representations
// of concrete values, references to external secrets (which are expanded at this point), or references
// to values in environment variables. The last should be injected into the environment of the container
// by appropriate EnvVar entries.
func ExpandValueForTemplate(v ClusterConfigTemplateValue) (vectorizedv1alpha1.YAMLRepresentation, error) {
	switch {
	case v.Representation != nil:
		return *v.Representation, nil
	case v.EnvVarRef != nil:
		if value, exist := os.LookupEnv(*v.EnvVarRef); exist {
			return vectorizedv1alpha1.YAMLRepresentation(value), nil
		}
		return "", fmt.Errorf("referenced environment variable is unset: %s", *v.EnvVarRef)
	case v.ExternalSecretRef != nil:
		// TODO: grab the external secret, yaml-serialise it [optional, depending on where the responsibility for this lies], return it
		return "", fmt.Errorf("don't know how to resolve external secret references")
	default:
		return "", fmt.Errorf("unspecified template value")
	}
}
