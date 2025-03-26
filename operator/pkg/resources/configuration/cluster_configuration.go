package configuration

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration
	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"

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
	cfg    map[string]vectorizedv1alpha1.ClusterConfigValue
	fixups []clusterconfiguration.Fixup
	err    error

	// These are created only once, on demand
	concrete     map[string]any
	templateFile []byte
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
	c.fixups = append(c.fixups, clusterconfiguration.Fixup{
		Field: field,
		CEL:   cel,
	})
}
