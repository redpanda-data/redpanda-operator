package clusterconfiguration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/operator/pkg/secrets"
)

func NewConfig(namespace string, reader k8sclient.Reader, cloudExpander *pkgsecrets.CloudExpander) *CombinedCfg {
	return &CombinedCfg{
		Node:    newNodeCfg(),
		Cluster: newClusterCfg(),

		namespace:     namespace,
		reader:        reader,
		cloudExpander: cloudExpander,
	}
}

type CombinedCfg struct {
	Node    *nodeCfg
	Cluster *clusterCfg

	// Runtime:
	volumes []corev1.Volume

	// initContainer settings
	// Mounts: these seem less likely
	// initMounts []corev1.VolumeMount
	// Bootstrap/initContainer-time support for k8s secret injection
	// This is required to inject things like the superuser names
	initEnvVars []corev1.EnvVar

	// Container settings
	// Mounts: eg, TLS secrets
	ctrMounts []corev1.VolumeMount
	// We *might* conceivable have env vars injected into the main runtime too, but it seems less likely
	// ctrEnvVars []corev1.EnvVar

	// Errors accumulated during construction
	errs []error

	// We expand templates only once
	templates map[string]string
	// We expand the environment for reification of templates only once
	env map[string]string

	// In order to achieve this, we may require the following
	namespace     string
	reader        k8sclient.Reader
	cloudExpander *pkgsecrets.CloudExpander
}

func (c *CombinedCfg) EnsureVolume(volume corev1.Volume) error {
	for _, v := range c.volumes {
		if v.Name == volume.Name {
			if reflect.DeepEqual(v, volume) {
				// Nothing to do
				return nil
			}
			err := fmt.Errorf("volume name repeated for %q", v.Name)
			c.errs = append(c.errs, err)
			return err
		}
	}

	c.volumes = append(c.volumes, volume)
	return nil
}

func (c *CombinedCfg) EnsureInitEnv(env corev1.EnvVar) error {
	for _, e := range c.initEnvVars {
		if e.Name == env.Name {
			if reflect.DeepEqual(e, env) {
				// Nothing to do
				return nil
			}
			err := fmt.Errorf("initContainer env name repeated for %q", e.Name)
			c.errs = append(c.errs, err)
			return err
		}
	}
	c.initEnvVars = append(c.initEnvVars, env)
	return nil
}

func (c *CombinedCfg) EnsureContainerMount(mount corev1.VolumeMount) error {
	for _, m := range c.ctrMounts {
		if m.Name == mount.Name {
			if reflect.DeepEqual(m, mount) {
				return nil
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

// Templates will finalise the set of any remaining env variables to add,
// then produce the serialised form for the template files to write out.
func (c *CombinedCfg) Templates() (map[string]string, error) {
	if c.templates != nil {
		return c.templates, nil
	}
	c.templates = make(map[string]string)
	if err := c.Node.template(c, c.templates); err != nil {
		return nil, fmt.Errorf("cannot template redpanda.yaml: %w", err)
	}
	if err := c.Cluster.template(c, c.templates); err != nil {
		return nil, fmt.Errorf("cannot template bootstrap.yaml: %w", err)
	}
	return c.templates, c.Error()
}

// AdditionalInitEnvVars will finalise the set of any remaining env variables to add,
// then return that final list.
func (c *CombinedCfg) AdditionalInitEnvVars() ([]corev1.EnvVar, error) {
	if _, err := c.Templates(); err != nil {
		return nil, err
	}
	return c.initEnvVars, nil
}

// Reification (that is, the concretising of all values). Since fixups may require environment variable
// settings - which come from k8s sources - and access to the cloud expander, and may rely upon secrets
// and other k8s resources being created - then this should be called as late as possible.

// constructEnv readies a simulated environment for CEL engine use in templating.
func (c *CombinedCfg) constructEnv(ctx context.Context) (map[string]string, error) {
	if c.env != nil {
		return c.env, nil
	}
	// force the final addition of any environment variables
	_, err := c.Templates()
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, e := range c.initEnvVars {
		switch {
		case e.ValueFrom == nil:
			// Add the plain value
			env[e.Name] = e.Value
		case e.ValueFrom.ConfigMapKeyRef != nil:
			var cm corev1.ConfigMap
			if err := c.reader.Get(ctx, k8sclient.ObjectKey{
				Namespace: c.namespace,
				Name:      e.ValueFrom.ConfigMapKeyRef.Name,
			}, &cm); err != nil {
				return nil, fmt.Errorf("resolving ConfigMapKeyRef for %q: %w", e.Name, err)
			}
			env[e.Name] = cm.Data[e.ValueFrom.ConfigMapKeyRef.Key]
		case e.ValueFrom.SecretKeyRef != nil:
			var cm corev1.Secret
			if err := c.reader.Get(ctx, k8sclient.ObjectKey{
				Namespace: c.namespace,
				Name:      e.ValueFrom.SecretKeyRef.Name,
			}, &cm); err != nil {
				return nil, fmt.Errorf("resolving SecretKeyRef for %q: %w", e.Name, err)
			}
			env[e.Name] = string(cm.Data[e.ValueFrom.SecretKeyRef.Key])
		}
	}
	c.env = env
	return c.env, nil
}

// ReifyNodeConfiguration evaluates the complete configurations, putting all secrets in place.
// This is intended only for in-process use: hashes of the results may be
// utilised, but otherwise these structures may contain secrets.
// Consequently, we want to avoid writing those out to any data-store (save
// the Redpanda Admin API)
func (c *CombinedCfg) ReifyNodeConfiguration(
	ctx context.Context,
) (nodeConfig *config.RedpandaYaml, err error) {
	environ, err := c.constructEnv(ctx)
	if err != nil {
		return nil, err
	}
	factory := StdLibFactory(ctx, environ, c.cloudExpander)
	return c.Node.reify(factory)
}

// GetNodeConfigHash returns md5 hash of the Node configuration.
// This is intended to be stable, because a change here will cause a sts restart.
func (c *CombinedCfg) GetNodeConfigHash(
	ctx context.Context,
) (string, error) {
	// Concretise the node configuration, weed out anything that would cause
	// a useless sts restart, and return the resulting hash.
	redpandaYaml, err := c.ReifyNodeConfiguration(ctx)
	if err != nil {
		return "", err
	}
	return nodeConfigurationHash(redpandaYaml)
}

// ReifyClusterConfiguration evaluates the complete configuration, putting all secrets in place.
// This is intended only for in-process use: hashes of the results may be
// utilised, but otherwise these structures may contain secrets.
// Consequently, we want to avoid writing those out to any data-store (save
// the Redpanda Admin API)
func (c *CombinedCfg) ReifyClusterConfiguration(
	ctx context.Context,
	schema rpadmin.ConfigSchema,
) (clusterConfig map[string]any, err error) {
	environ, err := c.constructEnv(ctx)
	if err != nil {
		return nil, err
	}
	factory := StdLibFactory(ctx, environ, c.cloudExpander)
	return c.Cluster.reify(factory, schema)
}

// GetCriticalClusterConfigHash returns md5 hash of the cluster configuration,
// considering only those elements that are declared to require a restart according
// to the supplied schema.
func (c *CombinedCfg) GetCriticalClusterConfigHash(
	ctx context.Context,
	schema rpadmin.ConfigSchema,
) (string, error) {
	// Concretise the node configuration, weed out anything that would cause
	// a useless sts restart, and return the resulting hash.
	clusterConfig, err := c.ReifyClusterConfiguration(ctx, schema)
	if err != nil {
		return "", err
	}
	return criticalClusterConfigurationHash(clusterConfig, schema)
}

// clone supplies a serialisation-backed object cloning mechanism for cases where
// the underlying type doesn't supply a `.Clone()` mechanism.
func clone[T any](val T) (T, error) {
	var res T
	buf, err := json.Marshal(val)
	if err != nil {
		return res, err
	}
	err = json.Unmarshal(buf, &res)
	return res, err
}
