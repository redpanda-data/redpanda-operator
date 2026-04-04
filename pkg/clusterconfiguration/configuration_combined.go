// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package clusterconfiguration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

func NewConfig(namespace string, reader k8sclient.Reader, cloudExpander *pkgsecrets.CloudExpander) *CombinedCfg {
	p := NewPodContext(namespace)
	return &CombinedCfg{
		PodContext: p,
		Node:       NewNodeCfg(p),
		Cluster:    NewClusterCfg(p),
		RPK:        NewRPKCfg(p),

		reader:        reader,
		cloudExpander: cloudExpander,
	}
}

func NewPodContext(namespace string) *PodContext {
	return &PodContext{
		namespace: namespace,
	}
}

type PodContext struct {
	namespace string

	// Runtime:
	volumes []corev1.Volume

	// initContainer settings
	// Mounts: these seem less likely
	// initMounts []corev1.VolumeMount
	// Bootstrap/initContainer-time support for k8s secret injection
	// This is required to inject things like the superuser names
	initEnvVars  []corev1.EnvVar
	initEnvFroms []corev1.EnvFromSource

	// Container settings
	// Mounts: eg, TLS secrets
	ctrMounts []corev1.VolumeMount
	// We *might* conceivable have env vars injected into the main runtime too, but it seems less likely
	// ctrEnvVars []corev1.EnvVar

	// Errors accumulated during construction
	errs []error

	// We expand the environment for reification of templates only once
	env map[string]string
}

type CombinedCfg struct {
	*PodContext
	Node    *nodeCfg
	Cluster *clusterCfg
	RPK     *rpkCfg

	// We expand templates only once
	templates map[string]string

	// In order to achieve this, we may require the following
	reader        k8sclient.Reader
	cloudExpander *pkgsecrets.CloudExpander
}

func (p *PodContext) EnsureVolume(volume corev1.Volume) error {
	for _, v := range p.volumes {
		if v.Name == volume.Name {
			if reflect.DeepEqual(v, volume) {
				// Nothing to do
				return nil
			}
			err := fmt.Errorf("volume name repeated for %q", v.Name)
			p.errs = append(p.errs, err)
			return err
		}
	}

	p.volumes = append(p.volumes, volume)
	return nil
}

func (p *PodContext) EnsureInitEnv(env corev1.EnvVar) error {
	for _, e := range p.initEnvVars {
		if e.Name == env.Name {
			if reflect.DeepEqual(e, env) {
				// Nothing to do
				return nil
			}
			err := fmt.Errorf("initContainer env name repeated for %q", e.Name)
			p.errs = append(p.errs, err)
			return err
		}
	}
	p.initEnvVars = append(p.initEnvVars, env)
	return nil
}

func (p *PodContext) EnsureInitEnvFrom(envFrom corev1.EnvFromSource) error {
	for _, e := range p.initEnvFroms {
		if reflect.DeepEqual(e, envFrom) {
			// Nothing to do
			return nil
		}
	}
	p.initEnvFroms = append(p.initEnvFroms, envFrom)
	return nil
}

func (p *PodContext) EnsureContainerMount(mount corev1.VolumeMount) error {
	for _, m := range p.ctrMounts {
		if m.Name == mount.Name {
			if reflect.DeepEqual(m, mount) {
				return nil
			}
			err := fmt.Errorf("container mount name repeated for %q", m.Name)
			p.errs = append(p.errs, err)
			return err
		}
	}
	p.ctrMounts = append(p.ctrMounts, mount)
	return nil
}

func (p *PodContext) Error() error {
	return errors.Join(p.errs...)
}

// SetAdditionalFlatProperty is to support the "centralized"-style of configuration updates.
// The additionalConfiguration attribute mixes cluster and node configuration.
func (c *CombinedCfg) SetAdditionalFlatProperty(key, repr string) error {
	if nodeProp := isKnownNodeProperty(key); !nodeProp && strings.HasPrefix(key, redpandaPropertyPrefix) {
		newKey := strings.TrimPrefix(key, redpandaPropertyPrefix)
		c.Cluster.Set(newKey, ClusterConfigValue{Repr: ptr.To(YAMLRepresentation(repr))})
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
	if err := c.Node.Template(c.templates); err != nil {
		return nil, fmt.Errorf("cannot template redpanda.yaml: %w", err)
	}
	if err := c.Cluster.Template(c.templates); err != nil {
		return nil, fmt.Errorf("cannot template bootstrap.yaml: %w", err)
	}
	if err := c.RPK.Template(c.templates); err != nil {
		return nil, fmt.Errorf("cannot template rpk.yaml: %w", err)
	}
	return c.templates, c.Error()
}

// AdditionalInitEnvVars will finalise the set of any remaining env variables to add,
// then return that final list.
// The list is always sorted by name; this is to ensure that map iteration order when
// constructing a configuration doesn't trigger a spurious sts restart.
func (c *CombinedCfg) AdditionalInitEnvVars() ([]corev1.EnvVar, error) {
	if _, err := c.Templates(); err != nil {
		return nil, err
	}
	sort.Slice(c.initEnvVars, func(i, j int) bool {
		return c.initEnvVars[i].Name < c.initEnvVars[j].Name
	})
	return c.initEnvVars, nil
}

// Reification (that is, the concretising of all values). Since fixups may require environment variable
// settings - which come from k8s sources - and access to the cloud expander, and may rely upon secrets
// and other k8s resources being created - then this should be called as late as possible.

// constructEnv readies a simulated environment for CEL engine use in templating.
func (p *PodContext) constructEnv(ctx context.Context, reader k8sclient.Reader) (map[string]string, error) {
	if p.env != nil {
		return p.env, nil
	}
	env := make(map[string]string)
	for _, e := range p.initEnvVars {
		switch {
		case e.ValueFrom == nil:
			// Add the plain value
			env[e.Name] = e.Value
		case e.ValueFrom.ConfigMapKeyRef != nil:
			var cm corev1.ConfigMap
			if err := reader.Get(ctx, k8sclient.ObjectKey{
				Namespace: p.namespace,
				Name:      e.ValueFrom.ConfigMapKeyRef.Name,
			}, &cm); err != nil {
				return nil, errors.WithStack(fmt.Errorf("resolving ConfigMapKeyRef for %q: %w", e.Name, err))
			}
			env[e.Name] = cm.Data[e.ValueFrom.ConfigMapKeyRef.Key]
		case e.ValueFrom.SecretKeyRef != nil:
			var cm corev1.Secret
			if err := reader.Get(ctx, k8sclient.ObjectKey{
				Namespace: p.namespace,
				Name:      e.ValueFrom.SecretKeyRef.Name,
			}, &cm); err != nil {
				return nil, errors.WithStack(fmt.Errorf("resolving SecretKeyRef for %q: %w", e.Name, err))
			}
			env[e.Name] = string(cm.Data[e.ValueFrom.SecretKeyRef.Key])
		default:
			// We don't know how to resolve this
			env[e.Name] = ""
		}
	}

	for _, src := range p.initEnvFroms {
		switch {
		case src.SecretRef != nil:
			ref := src.SecretRef
			key := client.ObjectKey{Namespace: p.namespace, Name: ref.Name}

			var secret corev1.Secret
			if err := reader.Get(ctx, key, &secret); err != nil {
				if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
					continue
				}
			}

			for k, v := range secret.Data {
				env[src.Prefix+k] = string(v)
			}

		case src.ConfigMapRef != nil:
			ref := src.ConfigMapRef
			key := client.ObjectKey{Namespace: p.namespace, Name: ref.Name}

			var cm corev1.ConfigMap
			if err := reader.Get(ctx, key, &cm); err != nil {
				if apierrors.IsNotFound(err) && ptr.Deref(ref.Optional, false) {
					continue
				}
			}

			for k, v := range cm.Data {
				env[src.Prefix+k] = v
			}
		}
	}

	p.env = env
	return env, nil
}

func (p *PodContext) constructFactory(ctx context.Context, reader k8sclient.Reader, cloudExpander *pkgsecrets.CloudExpander) (CelFactory, error) {
	environ, err := p.constructEnv(ctx, reader)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return StdLibFactory(ctx, environ, cloudExpander), nil
}

// ReifyNodeConfiguration evaluates the complete configurations, putting all secrets in place.
// This is intended only for in-process use: hashes of the results may be
// utilised, but otherwise these structures may contain secrets.
// Consequently, we want to avoid writing those out to any data-store (save
// the Redpanda Admin API)
func (c *CombinedCfg) ReifyNodeConfiguration(
	ctx context.Context,
) (nodeConfig *config.RedpandaYaml, err error) {
	return c.Node.Reify(ctx, c.reader, c.cloudExpander)
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
		return "", errors.WithStack(err)
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
	return c.Cluster.Reify(ctx, c.reader, c.cloudExpander, schema)
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
