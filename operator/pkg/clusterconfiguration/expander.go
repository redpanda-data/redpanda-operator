// Package clusterconfiguration holds types to track cluster configuration - the CRD declarations need to
// be transformed down into a representation of those values in a bootstrap template; we also supply an
// evaluator that can turn such a map of values into a map of raw concrete values.
package clusterconfiguration

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
)

// ExpandForConfiguration takes a ClusterConfiguration and expands all references,
// both to k8s secrets and to external secrets.
// The result may be used directly against the AdminAPI of a running cluster, but
// should not be persisted to any k8s resource, since it may contain secrets.
// The schema is used to interpret any external representations.
// TODO: this should operate like ExpandEnv, with additional secret resolution.
// We'll probably want a Reader or something similar to pull out k8s values.
func ExpandForConfiguration(
	ctx context.Context,
	reader client.Reader,
	cloudExpander *pkgsecrets.CloudExpander,
	namespace string,
	cfg vectorizedv1alpha1.ClusterConfiguration,
	schema rpadmin.ConfigSchema,
) (map[string]any, error) {
	properties := make(map[string]any, len(cfg))
	for k, v := range cfg {
		metadata := schema[k]
		switch {
		case v.Repr != nil:
			value, err := ParseRepresentation(string(*(v.Repr)), &metadata)
			if err != nil {
				return nil, fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err)
			}
			properties[k] = value
		case v.ConfigMapKeyRef != nil:
			var cm corev1.ConfigMap
			err := reader.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      v.ConfigMapKeyRef.Name,
			}, &cm)
			if err != nil {
				return nil, fmt.Errorf("configuration entry %q cannot read ConfigMap %q/%q: %w", k, namespace, v.ConfigMapKeyRef.Name, err)
			}
			repr, ok := cm.Data[v.ConfigMapKeyRef.Key]
			if !ok {
				return nil, fmt.Errorf("configuration entry %q: ConfigMap %q/%q has no key %q: %w", k, namespace, v.ConfigMapKeyRef.Name, v.ConfigMapKeyRef.Key, err)
			}
			value, err := ParseRepresentation(repr, &metadata)
			if err != nil {
				return nil, fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err)
			}
			properties[k] = value
		case v.SecretKeyRef != nil:
			var sec corev1.Secret
			err := reader.Get(ctx, client.ObjectKey{
				Namespace: namespace,
				Name:      v.SecretKeyRef.Name,
			}, &sec)
			if err != nil {
				return nil, fmt.Errorf("configuration entry %q cannot read Secret %q/%q: %w", k, namespace, v.SecretKeyRef.Name, err)
			}
			repr, ok := sec.Data[v.SecretKeyRef.Key]
			if !ok {
				return nil, fmt.Errorf("configuration entry %q: ConfigMap %q/%q has no key %q: %w", k, namespace, v.SecretKeyRef.Name, v.SecretKeyRef.Key, err)
			}
			value, err := ParseRepresentation(string(repr), &metadata)
			if err != nil {
				return nil, fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err)
			}
			properties[k] = value
		case v.ExternalSecretRef != nil:
			if cloudExpander == nil {
				return nil, errors.New(fmt.Sprintf("configuration entry %q: external secret provided but the expander was not configured", k))
			}
			expanded, err := cloudExpander.Expand(ctx, *v.ExternalSecretRef)
			if err != nil {
				return nil, fmt.Errorf("configuration entry %q: trouble expanding external secret reference: %w", k, err)
			}
			value, err := ParseRepresentation(expanded, &metadata)
			if err != nil {
				return nil, fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err)
			}
			properties[k] = value
		default:
			return nil, fmt.Errorf("unrecognised configuration entry for key %q", k)
		}
	}
	return properties, nil
}

func ParseRepresentation(repr string, metadata *rpadmin.ConfigPropertyMetadata) (any, error) {
	if metadata.Nullable && repr == "null" {
		return nil, nil
	}
	switch metadata.Type {
	case "string":
		var s string
		// YAML unmarshalling to "be liberal in what we accept" here.
		err := yaml.Unmarshal([]byte(repr), &s)
		return s, err
	case "number":
		return strconv.ParseFloat(strings.Trim(repr, "\n"), 64)
	case "integer":
		return strconv.ParseInt(strings.Trim(repr, "\n"), 10, 64)
	case "boolean":
		return strconv.ParseBool(strings.Trim(repr, "\n"))
	case "array":
		return convertStringToStringArray(repr)
	default:
		// TODO: It's unclear whether we should let this ride, or report an error.
		// By letting it pass, we will ultimately report an Unknown value on the condition.
		var s string
		err := yaml.Unmarshal([]byte(repr), &s)
		return s, err
		// Strict alternative:
		// return nil, fmt.Errorf("unrecognised configuration type: %s", metadata.Type)
	}
}

// convertStringToStringArray duplicates the v1 string->[string] processing
func convertStringToStringArray(value string) ([]string, error) {
	a := make([]string, 0)
	err := yaml.Unmarshal([]byte(value), &a)

	if len(a) == 1 {
		// it is possible this was not comma separated, so let's make it so and retry unmarshalling
		b := make([]string, 0)
		errB := yaml.Unmarshal([]byte(strings.ReplaceAll(value, " ", ",")), &b)
		if errB == nil && len(b) > len(a) {
			sort.Strings(b)
			return b, errB
		}
	}
	sort.Strings(a)
	return a, err
}
