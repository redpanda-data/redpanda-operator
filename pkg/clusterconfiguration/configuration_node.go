package clusterconfiguration

import (
	"context"
	"crypto/md5" //nolint:gosec // this is not encrypting secure info
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

func NewNodeCfg(p *PodContext) *nodeCfg {
	return &nodeCfg{
		PodContext:   p,
		RedpandaYaml: config.ProdDefault(),
	}
}

type nodeCfg struct {
	*PodContext
	*config.RedpandaYaml
	fixups []Fixup

	// These are created only once, on demand
	concrete *config.RedpandaYaml
}

// SetAdditionalConfiguration supplies the legacy support of the additionalConfiguration field
func (n *nodeCfg) SetAdditionalConfiguration(k, v string) error {
	// Add arbitrary parameters to configuration
	if builtInType(v) {
		err := config.Set(n.RedpandaYaml, k, v)
		if err != nil {
			return fmt.Errorf("setting built-in type: %w", err)
		}
	} else if !skipNodeSpecificConfiguration(v) {
		err := config.Set(n.RedpandaYaml, k, v)
		if err != nil {
			return fmt.Errorf("setting complex type: %w", err)
		}
	}
	return nil
}

// builtInType supports SetAdditionalConfiguration
func builtInType(value string) bool {
	if _, err := strconv.Atoi(value); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseBool(value); err == nil {
		return true
	}
	return false
}

// skipNodeSpecificConfiguration is a legacy hack to avoid overwriting node-specific configuration
// during the configuration patch stage.
// It returns whether to skip node specific configuration in handling additional configuration.
// Node specific configuration contains variable like {{ .Index }}
// e.g. "[{'name':'private-link','address':'{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 }}.redpanda.com','port': 'port': {{30092 | add .Index}}}]"
func skipNodeSpecificConfiguration(cfg string) bool {
	return strings.Contains(cfg, ".Index")
}

func (n *nodeCfg) AddFixup(field, cel string) {
	n.fixups = append(n.fixups, Fixup{
		Field: field,
		CEL:   cel,
	})
}

const (
	RedpandaYamlTemplateFile = "redpanda.yaml"
	RedpandaYamlFixupFile    = "redpanda.yaml.fixups"
)

func (n *nodeCfg) Template(contents map[string]string) error {
	rpConfig, err := yaml.Marshal(n.RedpandaYaml)
	if err != nil {
		return fmt.Errorf("could not serialize node config: %w", err)
	}
	contents[RedpandaYamlTemplateFile] = string(rpConfig)
	if n.fixups == nil {
		n.fixups = []Fixup{}
	}
	fixups, err := json.Marshal(n.fixups)
	if err != nil {
		return fmt.Errorf("could not serialize node fixups: %w", err)
	}
	contents[RedpandaYamlFixupFile] = string(fixups)
	return nil
}

// Reify is used to turn a template into a fully-filled structure,
// complete with any secret fixups in place.
func (n *nodeCfg) Reify(ctx context.Context, reader k8sclient.Reader, cloudExpander *pkgsecrets.CloudExpander) (*config.RedpandaYaml, error) {
	if n.concrete != nil {
		return n.concrete, nil
	}
	factory, err := n.constructFactory(ctx, reader, cloudExpander)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// Clone the template so as to not overwrite it
	cfg, err := clone(n.RedpandaYaml)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	t := Template[*config.RedpandaYaml]{
		Content: cfg,
		Fixups:  n.fixups,
	}
	if err := t.Fixup(factory); err != nil {
		return nil, errors.WithStack(err)
	}
	n.concrete = cfg
	return n.concrete, nil
}

// Handling for hashing

// nodeConfigurationHash computes a hash of the node configuration considering only node properties
// but excluding fields that trigger unnecessary restarts.
// This will
// Moved from pkg/resources.
func nodeConfigurationHash(redpandaYaml *config.RedpandaYaml) (string, error) {
	redpandaYaml, err := clone(redpandaYaml)
	if err != nil {
		return "", err
	}
	// clean any cluster property from config before serializing
	removeFieldsThatShouldNotTriggerRestart(redpandaYaml)
	props := redpandaYaml.Redpanda.Other
	// TODO: The following should have no effect; it's a hold-over from the combined configuration days.
	redpandaYaml.Redpanda.Other = make(map[string]interface{})
	for k, v := range props {
		if isKnownNodeProperty(fmt.Sprintf("%s%s", redpandaPropertyPrefix, k)) {
			redpandaYaml.Redpanda.Other[k] = v
		}
	}

	serialized, err := yaml.Marshal(redpandaYaml)
	if err != nil {
		return "", err
	}
	md5Hash := md5.Sum(serialized) //nolint:gosec // this is not encrypting secure info
	return fmt.Sprintf("%x", md5Hash), nil
}

// Ignore seeds in the hash computation such that any seed changes do not
// trigger a rolling restart across the nodes.
func removeFieldsThatShouldNotTriggerRestart(redpandaYaml *config.RedpandaYaml) {
	redpandaYaml.Redpanda.SeedServers = []config.SeedServer{}
}

const redpandaPropertyPrefix = "redpanda."

var knownNodeProperties map[string]bool

func isKnownNodeProperty(prop string) bool {
	if v, ok := knownNodeProperties[prop]; ok {
		return v
	}
	for k := range knownNodeProperties {
		if strings.HasPrefix(prop, fmt.Sprintf("%s.", k)) {
			return true
		}
	}
	return false
}

func init() {
	knownNodeProperties = make(map[string]bool)

	// The assumption here is that all explicit fields of RedpandaNodeConfig are node properties
	cfg := reflect.TypeOf(config.RedpandaNodeConfig{})
	for i := 0; i < cfg.NumField(); i++ {
		tag := cfg.Field(i).Tag
		yamlTag := tag.Get("yaml")
		parts := strings.Split(yamlTag, ",")
		if len(parts) > 0 && parts[0] != "" {
			knownNodeProperties[fmt.Sprintf("redpanda.%s", parts[0])] = true
		}
	}
}
