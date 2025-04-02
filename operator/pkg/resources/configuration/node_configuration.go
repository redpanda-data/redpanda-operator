package configuration

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/clusterconfiguration"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
)

func newNodeCfg() *nodeCfg {
	return &nodeCfg{
		Cfg: config.ProdDefault(),
	}
}

type nodeCfg struct {
	Cfg    *config.RedpandaYaml
	fixups []clusterconfiguration.Fixup
	// Errors accumulated during construction
	err error

	// These are created only once, on demand
	concrete     *config.RedpandaYaml
	templateFile []byte
}

// SetAddtionalConfiguration supplies the legacy support of the additionalConfiguration field
func (n *nodeCfg) SetAdditionalConfiguration(k, v string) error {
	// Add arbitrary parameters to configuration
	if builtInType(v) {
		err := config.Set(n.Cfg, k, v)
		if err != nil {
			return fmt.Errorf("setting built-in type: %w", err)
		}
	} else if !skipNodeSpecificConfiguration(v) {
		err := config.Set(n.Cfg, k, v)
		if err != nil {
			return fmt.Errorf("setting complex type: %w", err)
		}
	}
	return nil
}

func (n *nodeCfg) AddFixup(field, cel string) {
	n.fixups = append(n.fixups, clusterconfiguration.Fixup{
		Field: field,
		CEL:   cel,
	})
}
