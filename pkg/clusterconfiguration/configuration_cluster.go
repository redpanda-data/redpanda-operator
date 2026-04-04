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
	"maps"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/common-go/rpadmin"
	"gopkg.in/yaml.v3" //nolint:depguard // this is necessary due to differences in how the yaml tagging mechanisms work and the fact that some structs on config.RedpandaYaml are missing inline annotations
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

func NewClusterCfg(p *PodContext) *clusterCfg {
	return &clusterCfg{
		PodContext: p,
		templated:  make(map[string]string),
		fixups:     []Fixup{},
	}
}

type clusterCfg struct {
	*PodContext
	// We compile ClusterConfigValue entries into templates immediately
	templated map[string]string
	fixups    []Fixup
	errs      []error

	// These are created only once, on demand
	concrete map[string]any
}

func (c *clusterCfg) Error() error {
	return errors.Join(c.errs...)
}

func (c *clusterCfg) Set(k string, v ClusterConfigValue) {
	if v == (ClusterConfigValue{}) {
		delete(c.templated, k)
		return
	}
	if t := c.compile(k, v); t != nil {
		c.templated[k] = *t
	}
}

// SetAdditionalConfiguration offers legacy support for the additionalConfiguration
// attribute of the CR.
func (c *clusterCfg) SetAdditionalConfiguration(k, repr string) {
	c.Set(k, ClusterConfigValue{
		Repr: ptr.To(YAMLRepresentation(repr)),
	})
}

func AppendValue[T any](c *clusterCfg, k string, v T) error {
	var values []T
	entry, found := c.templated[k]
	if found {
		// We use yaml unmarshalling and json marshalling in order to be "liberal in what we accept, and conservative
		// in what we send" here.
		if err := yaml.Unmarshal([]byte(entry), &values); err != nil {
			err = fmt.Errorf("cannot append value to malformed cluster configuration attribute %q: %w", k, err)
			c.errs = append(c.errs, err)
			return err
		}
	}
	values = append(values, v)
	buf, err := json.Marshal(values)
	if err != nil {
		err = fmt.Errorf("cannot marshal list with append value for cluster configuration attribute %q: %w", k, err)
		c.errs = append(c.errs, err)
		return err
	}
	c.templated[k] = string(buf)
	return nil
}

// SetValue takes a raw value and serialises it, for use in the bootstrap template.
func (c *clusterCfg) SetValue(k string, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		err = fmt.Errorf("cannot marshal value for cluster configuration attribute %q: %w", k, err)
		c.errs = append(c.errs, err)
		return err
	}
	c.Set(k, ClusterConfigValue{
		Repr: ptr.To(YAMLRepresentation(buf)),
	})
	return nil
}

func (c *clusterCfg) AddFixup(field, cel string) {
	c.fixups = append(c.fixups, Fixup{
		Field: field,
		CEL:   cel,
	})
}

func (c *clusterCfg) compile(k string, v ClusterConfigValue) *string {
	if c.fixups == nil {
		c.fixups = []Fixup{}
	}

	// Ensure any references to k8s contents are env-expandable.
	// Inject fixups as appropriate.
	switch {
	case v.Repr != nil:
		return ptr.To(string(*v.Repr))
	case v.ConfigMapKeyRef != nil:
		envName := keyToEnvVar(k)
		if err := c.EnsureInitEnv(corev1.EnvVar{
			Name: envName,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: v.ConfigMapKeyRef,
			},
		}); err != nil {
			c.errs = append(c.errs, fmt.Errorf("compiling ConfigMapRef %q: %w", k, err))
			return nil
		}
		// We assume by default that the supplied value is a raw string, which can and should be quoted for the safe
		// insertion into a bootstrap template.
		// If that's not the case, and the referred value's octets should be injected into the template verbatim,
		// then the user can specify that explicitly.
		if v.UseRawValue {
			c.AddFixup(k, fmt.Sprintf(`%s("%s")`, CELEnvString, envName))
		} else {
			c.AddFixup(k, fmt.Sprintf(`%s(%s("%s"))`, CELRepr, CELEnvString, envName))
		}
		return ptr.To(``)
	case v.SecretKeyRef != nil:
		envName := keyToEnvVar(k)
		if err := c.EnsureInitEnv(corev1.EnvVar{
			Name: envName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: v.SecretKeyRef,
			},
		}); err != nil {
			c.errs = append(c.errs, fmt.Errorf("compiling SecretRef %q: %w", k, err))
			return nil
		}
		// We assume by default that the supplied value is a raw string, which can and should be quoted for the safe
		// insertion into a bootstrap template.
		// If that's not the case, and the referred value's octets should be injected into the template verbatim,
		// then the user can specify that explicitly.
		if v.UseRawValue {
			c.AddFixup(k, fmt.Sprintf(`%s("%s")`, CELEnvString, envName))
		} else {
			c.AddFixup(k, fmt.Sprintf(`%s(%s("%s"))`, CELRepr, CELEnvString, envName))
		}
		return ptr.To(``)
	case v.ExternalSecretRef != nil: // nolint:staticcheck // ignore deprecation for now
		v.ExternalSecretRefSelector = &ExternalSecretKeySelector{
			Name: *v.ExternalSecretRef, // nolint:staticcheck // ignore deprecation for now
		}
		fallthrough
	case v.ExternalSecretRefSelector != nil:
		// We assume by default that the supplied value is a raw string, which can and should be quoted for the safe
		// insertion into a bootstrap template.
		// If that's not the case, and the referred value's octets should be injected into the template verbatim,
		// then the user can specify that explicitly.
		// We wrap the returned value in `errorToWarning` in the case where the key is marked as optional.
		fixup := fmt.Sprintf(`%s("%s")`, CELExternalSecretRef, v.ExternalSecretRefSelector.Name)
		if !v.UseRawValue {
			fixup = fmt.Sprintf(`%s(%s)`, CELRepr, fixup)
		}
		if ptr.Deref(v.ExternalSecretRefSelector.Optional, false) {
			fixup = fmt.Sprintf(`%s(%s)`, CELErrorToWarning, fixup)
		}
		c.AddFixup(k, fixup)
	}
	return nil
}

const (
	BootstrapTemplateFile = ".bootstrap.json.in"
	BootstrapFixupFile    = "bootstrap.yaml.fixups"
	BootstrapTargetFile   = ".bootstrap.yaml"
)

func (c *clusterCfg) Template(contents map[string]string) error {
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

// Reify is used to turn a template into a fully-filled structure,
// complete with any secret fixups in place.
func (c *clusterCfg) Reify(ctx context.Context, reader k8sclient.Reader, cloudExpander *pkgsecrets.CloudExpander, schema rpadmin.ConfigSchema) (map[string]any, error) {
	if c.concrete != nil {
		return c.concrete, nil
	}

	factory, err := c.constructFactory(ctx, reader, cloudExpander)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// We turn the templated value into a map[string]string to begin with - this is what the
	// cluster configuration fixups expect.
	representations := maps.Clone(c.templated)

	// Now run the cluster configuration fixups
	t := Template[map[string]string]{
		Content: representations,
		Fixups:  c.fixups,
	}
	if err := t.Fixup(factory); err != nil {
		return nil, errors.WithStack(err)
	}

	// Finally, use the schema to turn those representations into concrete values
	properties := make(map[string]any, len(representations))
	for k, v := range representations {
		metadata := schema[k]
		value, err := ParseRepresentation(v, &metadata)
		if err != nil {
			return nil, errors.WithStack(fmt.Errorf("trouble converting configuration entry %q to value: %w", k, err))
		}
		properties[k] = value
	}
	c.concrete = properties
	return properties, nil
}
