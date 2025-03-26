package configuration

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
)

func NewConfig() *CombinedCfg {
	return &CombinedCfg{
		Node:    newNodeCfg(),
		Cluster: newClusterCfg(),
	}
}

type CombinedCfg struct {
	Node    *nodeCfg
	Cluster *clusterCfg

	// Runtime:
	volumes []corev1.Volume

	// initContainer settings
	// Mounts: these seem less likely
	initMounts []corev1.VolumeMount
	// Bootstrap/initContainer-time support for k8s secret injection
	// This is required to inject things like the superuser names
	initEnvVars []corev1.EnvVar

	// Container settings
	// Mounts: eg, TLS secrets
	ctrMounts []corev1.VolumeMount
	// We *might* conceivable have env vars injected into the main runtime too, but it seems less likely
	ctrEnvVars []corev1.EnvVar

	// Errors accumulated during construction
	errs []error
}

func (c *CombinedCfg) AddVolume(volume corev1.Volume) error {
	for _, v := range c.volumes {
		if v.Name == volume.Name {
			if reflect.DeepEqual(v, volume) {
				break
			}
			err := fmt.Errorf("volume name repeated for %q", v.Name)
			c.errs = append(c.errs, err)
			return err
		}
	}

	c.volumes = append(c.volumes, volume)
	return nil
}

func (c *CombinedCfg) AddInitEnv(env corev1.EnvVar) error {
	for _, e := range c.initEnvVars {
		if e.Name == env.Name {
			if reflect.DeepEqual(e, env) {
				break
			}
			err := fmt.Errorf("initContainer env name repeated for %q", e.Name)
			c.errs = append(c.errs, err)
			return err
		}
	}
	c.initEnvVars = append(c.initEnvVars, env)
	return nil
}

func (c *CombinedCfg) AddContainerMount(mount corev1.VolumeMount) error {
	for _, m := range c.ctrMounts {
		if m.Name == mount.Name {
			if reflect.DeepEqual(m, mount) {
				break
			}
			err := fmt.Errorf("container mount name repeated for %q", m.Name)
			c.errs = append(c.errs, err)
			return err
		}
	}
	c.ctrMounts = append(c.ctrMounts, mount)
	return nil
}

func (c *CombinedCfg) Error() error {
	return errors.Join(c.errs...)
}

// SetAdditionalFlatProperty is to support the "centralized"-style of configuration updates.
// The additionalConfiguration attribute mixes cluster and node configuration.
func (c *CombinedCfg) SetAdditionalFlatProperty(key, repr string) error {
	if nodeProp := isKnownNodeProperty(key); !nodeProp && strings.HasPrefix(key, redpandaPropertyPrefix) {
		newKey := strings.TrimPrefix(key, redpandaPropertyPrefix)
		c.Cluster.Set(newKey, vectorizedv1alpha1.ClusterConfigValue{Repr: ptr.To(vectorizedv1alpha1.YAMLRepresentation(repr))})
		return nil
	}
	return c.Node.SetAdditionalConfiguration(key, repr)
}
