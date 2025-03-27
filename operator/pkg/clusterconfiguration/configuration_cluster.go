package clusterconfiguration

import (
	"crypto/md5" //nolint:gosec // this is not encrypting secure info
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

func newClusterCfg() *clusterCfg {
	return &clusterCfg{
		cfg: make(map[string]vectorizedv1alpha1.ClusterConfigValue),
	}
}

type clusterCfg struct {
	// Required for fixup compi
	cfg    map[string]vectorizedv1alpha1.ClusterConfigValue
	fixups []Fixup
	err    error

	// These are created only once, on demand
	templated map[string]vectorizedv1alpha1.YAMLRepresentation
	concrete  map[string]any
}

func (c *clusterCfg) Error() error {
	return c.err
}

func (c *clusterCfg) Set(k string, v vectorizedv1alpha1.ClusterConfigValue) {
	c.cfg[k] = v
}

// SetAdditionalConfiguration offers legacy support for the additionalConfiguration
// attribute of the CR.
func (c *clusterCfg) SetAdditionalConfiguration(k, repr string) {
	c.cfg[k] = vectorizedv1alpha1.ClusterConfigValue{
		Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation(repr)),
	}
}

func AppendValue[T any](c *clusterCfg, k string, v T) error {
	var values []T
	entry, found := c.cfg[k]
	if found {
		// Ensure this only has a representation thus far, no other references
		if entry.ConfigMapKeyRef != nil || entry.SecretKeyRef != nil || entry.ExternalSecretRef != nil {
			err := fmt.Errorf("cannot append value to mixed-type cluster configuration attribute %q", k)
			c.err = errors.Join(c.err, err)
			return err
		}
		if entry.Repr == nil {
			err := fmt.Errorf("cannot append value to missing cluster configuration attribute %q", k)
			c.err = errors.Join(c.err, err)
			return err
		}
		// We use yaml unmarshalling and json marshalling in order to be "liberal in what we accept, and conservative
		// in what we send" here.
		if err := yaml.Unmarshal([]byte(*entry.Repr), &values); err != nil {
			err = fmt.Errorf("cannot append value to malformed cluster configuration attribute %q: %w", k, err)
			c.err = errors.Join(c.err, err)
			return err
		}
	}
	values = append(values, v)
	buf, err := json.Marshal(values)
	if err != nil {
		err = fmt.Errorf("cannot marshal list with append value for cluster configuration attribute %q: %w", k, err)
		c.err = errors.Join(c.err, err)
		return err
	}
	c.cfg[k] = vectorizedv1alpha1.ClusterConfigValue{
		Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation(buf)),
	}
	return nil
}

// SetValue takes a raw value and serialises it, for use in the bootstrap template.
func (c *clusterCfg) SetValue(k string, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		err = fmt.Errorf("cannot marshal value for cluster configuration attribute %q: %w", k, err)
		c.err = errors.Join(c.err, err)
		return err
	}
	c.cfg[k] = vectorizedv1alpha1.ClusterConfigValue{
		Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation(buf)),
	}
	return nil
}

func (c *clusterCfg) AddFixup(field, cel string) {
	c.fixups = append(c.fixups, Fixup{
		Field: field,
		CEL:   cel,
	})
}

func (c *clusterCfg) finalize(parent *CombinedCfg) error {
	if c.templated != nil {
		return nil
	}
	c.templated = make(map[string]vectorizedv1alpha1.YAMLRepresentation)
	if c.fixups == nil {
		c.fixups = []Fixup{}
	}

	// Ensure any references to k8s contents are env-expandable.
	// Inject fixups as appropriate.
	for k, v := range c.cfg {
		switch {
		case v.Repr != nil:
			c.templated[k] = *v.Repr
		case v.ConfigMapKeyRef != nil:
			envName := keyToEnvVar(k)
			if err := parent.EnsureInitEnv(corev1.EnvVar{
				Name: envName,
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: v.ConfigMapKeyRef,
				},
			}); err != nil {
				return fmt.Errorf("compiling ConfigMapRef %q: %w", k, err)
			}
			c.templated[k] = vectorizedv1alpha1.YAMLRepresentation(`""`)
			// NOTE: this is an env var reference. Assuming the value's intended to be interpreted as a string, we may want CEL to wrap a `repr(..)` around this.
			c.AddFixup(k, fmt.Sprintf(`%s("%s")`, CELEnvString, envName))
		case v.SecretKeyRef != nil:
			envName := keyToEnvVar(k)
			if err := parent.EnsureInitEnv(corev1.EnvVar{
				Name: envName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: v.SecretKeyRef,
				},
			}); err != nil {
				return fmt.Errorf("compiling SecretRef %q: %w", k, err)
			}
			c.templated[k] = vectorizedv1alpha1.YAMLRepresentation(`""`)
			// NOTE: this is an env var reference. Assuming the value's intended to be interpreted as a string, we want CEL to wrap a `repr(..)` around this.
			c.AddFixup(k, fmt.Sprintf(`%s("%s")`, CELEnvString, envName))
		case v.ExternalSecretRef != nil:
			c.templated[k] = vectorizedv1alpha1.YAMLRepresentation(`""`)
			// NOTE: this is an external secret reference. Assuming the value's intended to be interpreted as a string, we want CEL to wrap a `repr(..)` around this.
			c.AddFixup(k, fmt.Sprintf(`%s(%s(%s("%s")))`, CELRepr, CELExternalSecretRef, CELEnvString, *v.ExternalSecretRef))
		}
	}
	return nil
}

const (
	BootstrapTemplateFile = ".bootstrap.json.in"
	BootstrapFixupFile    = "bootstrap.yaml.fixups"
)

func (c *clusterCfg) template(parent *CombinedCfg, contents map[string]string) error {
	if err := c.finalize(parent); err != nil {
		return err
	}

	// Legacy file; leave it behind for the moment.
	contents[".bootstrap.yaml"] = ""
	bootstrapTemplate, err := json.Marshal(c.templated)
	if err != nil {
		return fmt.Errorf("could not serialize cluster config: %w", err)
	}
	contents[BootstrapTemplateFile] = string(bootstrapTemplate)
	fixups, err := json.Marshal(c.fixups)
	if err != nil {
		return fmt.Errorf("could not serialize cluster fixups: %w", err)
	}
	contents[BootstrapFixupFile] = string(fixups)
	return nil
}

func keyToEnvVar(k string) string {
	return "REDPANDA_" + strings.ReplaceAll(strings.ToUpper(k), ".", "_")
}

// reify is used to turn a template into a fully-filled structure,
// complete with any secret fixups in place.
func (c *clusterCfg) reify(engineFactory CelFactory, schema rpadmin.ConfigSchema) (map[string]any, error) {
	if c.concrete != nil {
		return c.concrete, nil
	}

	// We turn the templated value into a map[string]string to begin with - this is what the
	// cluster configuration fixups expect.
	representations := make(map[string]string, len(c.templated))
	for k, v := range c.templated {
		representations[k] = string(v)
	}

	// Now run the cluster configuration fixups
	t := Template[map[string]string]{
		Content: representations,
		Fixups:  c.fixups,
	}
	if err := t.Fixup(engineFactory); err != nil {
		return nil, err
	}

	// Finally, use the schema to turn those representations into concrete values
	properties := make(map[string]any, len(representations))
	for k, v := range representations {
		metadata := schema[k]
		value, err := ParseRepresentation(v, &metadata)
		if err != nil {
			return nil, fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err)
		}
		properties[k] = value
	}
	c.concrete = properties
	return properties, nil
}

// criticalClusterConfigurationHash is a short-term helper to handle checking of cluster configuration.
// It hashes only properties that require a restart.
func criticalClusterConfigurationHash(
	concreteCfg map[string]any,
	schema rpadmin.ConfigSchema,
) (string, error) {
	// Ignore cluster properties that don't require restart
	criticalCfg := make(map[string]any)
	for k, v := range concreteCfg {
		// Unknown properties should be ignored as they might be user errors
		if meta, ok := schema[k]; ok && meta.NeedsRestart {
			criticalCfg[k] = v
		}
	}
	// Hash this using the json-marshalled format.
	buf, err := json.Marshal(criticalCfg)
	if err != nil {
		return "", err
	}
	// We keep using md5 for having the same format as node hash
	md5Hash := md5.Sum(buf) //nolint:gosec // this is not encrypting secure info
	return fmt.Sprintf("%x", md5Hash), nil
}
