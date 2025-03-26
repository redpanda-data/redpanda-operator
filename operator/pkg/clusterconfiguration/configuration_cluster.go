package clusterconfiguration

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"gopkg.in/yaml.v3"
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
	templated map[string]ClusterConfigTemplateValue
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

func SetValue[T any](c *clusterCfg, k string, v T) error {
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
	c.templated = make(map[string]ClusterConfigTemplateValue)
	if c.fixups == nil {
		c.fixups = []Fixup{}
	}

	// Ensure any references to k8s contents are env-expandable.
	// Inject fixups as appropriate.
	for k, v := range c.cfg {
		switch {
		case v.Repr != nil:
			c.templated[k] = ClusterConfigTemplateValue{
				Representation: v.Repr,
			}
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
			c.templated[k] = ClusterConfigTemplateValue{
				Representation: ptr.To(vectorizedv1alpha1.YAMLRepresentation(`""`)),
			}
			// TODO: this is an env var reference. Assuming the value's intended to be interpreted as a string, we might want CEL to wrap a `repr(..)` around this.
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
			c.templated[k] = ClusterConfigTemplateValue{
				Representation: ptr.To(vectorizedv1alpha1.YAMLRepresentation(`""`)),
			}
			// TODO: this is an env var reference. Assuming the value's intended to be interpreted as a string, we might want CEL to wrap a `repr(..)` around this.
			c.AddFixup(k, fmt.Sprintf(`%s("%s")`, CELEnvString, envName))
		case v.ExternalSecretRef != nil:
			c.templated[k] = ClusterConfigTemplateValue{
				Representation: ptr.To(vectorizedv1alpha1.YAMLRepresentation(`""`)),
			}
			// TODO: this is an external secret reference. Assuming the value's intended to be interpreted as a string, we might want CEL to wrap a `repr(..)` around this.
			c.AddFixup(k, fmt.Sprintf(`%s(%s("%s"))`, CELExternalSecretRef, CELEnvString, *v.SecretKeyRef))
		}
	}
	return nil
}

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
	contents[".bootstrap.json.in"] = string(bootstrapTemplate)
	fixups, err := json.Marshal(c.fixups)
	if err != nil {
		return fmt.Errorf("could not serialize cluster fixups: %w", err)
	}
	contents["bootstrap.yaml.fixups"] = string(fixups)
	return nil
}

func keyToEnvVar(k string) string {
	return "REDPANDA_" + strings.Replace(strings.ToUpper(k), ".", "_", -1)
}
