package configuration

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	corev1 "k8s.io/api/core/v1"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// ExpandForBootstrap passes over the input ClusterConfiguration and returns one with
// all ConfigMapKeyRef and SecretKeyRef entries turned into environment references,
// together with a list of env var definitions required to satisfy those.
func ExpandForBootstrap(cfg map[string]vectorizedv1alpha1.ClusterConfigValue) (map[string]vectorizedv1alpha1.ClusterConfigValue, []corev1.EnvVar, error) {
	expanded := make(map[string]vectorizedv1alpha1.ClusterConfigValue, len(cfg))
	ensureVar := func(k string, v vectorizedv1alpha1.ClusterConfigValue) string {
		// If this entry already has an environment variable, keep it.
		if v.EnvVarRef != nil {
			return *v.EnvVarRef
		}
		return "REDPANDA_" + strings.Replace(strings.ToUpper(k), ".", "_", -1)
	}
	var envs []corev1.EnvVar
	for k, v := range cfg {
		switch {
		case v.Representation != nil:
			expanded[k] = vectorizedv1alpha1.ClusterConfigValue{
				Representation: v.Representation,
			}
		case v.ConfigMapKeyRef != nil:
			envName := ensureVar(k, v)
			expanded[k] = vectorizedv1alpha1.ClusterConfigValue{
				EnvVarRef: &envName,
			}
			envs = append(envs, corev1.EnvVar{
				Name: envName,
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: v.ConfigMapKeyRef,
				},
			})
		case v.SecretKeyRef != nil:
			envName := ensureVar(k, v)
			expanded[k] = vectorizedv1alpha1.ClusterConfigValue{
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
			expanded[k] = vectorizedv1alpha1.ClusterConfigValue{
				ExternalSecretRef: v.ExternalSecretRef,
			}
		default:
			return nil, nil, fmt.Errorf("bad cluster config value for %s", k)
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
func ExpandForConfiguration(cfg map[string]vectorizedv1alpha1.ClusterConfigValue, schema rpadmin.ConfigSchema) (map[string]any, error) {
	return nil, fmt.Errorf("unimplemented")
}

// ExpandValueForTemplate is intended to run in an initContainer. All values are either representations
// of concrete values, references to external secrets (which are expanded at this point), or references
// to values in environment variables. The last should be injected into the environment of the container
// by appropriate EnvVar entries.
func ExpandValueForTemplate(v vectorizedv1alpha1.ClusterConfigValue) (vectorizedv1alpha1.YamlRepresentation, error) {
	switch {
	case v.Representation != nil:
		return *v.Representation, nil
	case v.ConfigMapKeyRef != nil:
		return "", fmt.Errorf("don't know how to resolve config map references")
	case v.SecretKeyRef != nil:
		return "", fmt.Errorf("don't know how to resolve secret references")
	case v.EnvVarRef != nil:
		if value, exist := os.LookupEnv(*v.EnvVarRef); exist {
			return vectorizedv1alpha1.YamlRepresentation(value), nil
		}
		return "", fmt.Errorf("referenced environment variable is unset: %s", *v.EnvVarRef)
	case v.ExternalSecretRef != nil:
		// TODO: grab the external secret, yaml-serialise it [optional, depending on where the responsibility for this lies], return it
		return "", fmt.Errorf("don't know how to resolve external secret references")
	default:
		return "", fmt.Errorf("unspecified template value")
	}
}
