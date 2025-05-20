package clusterconfiguration

import (
	"encoding/json"
	"fmt"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"gopkg.in/yaml.v3"
)

func NewRPKCfg(p *PodContext) *rpkCfg {
	y := config.RpkYaml{
		Version:    7,
		Profiles:   []config.RpkProfile{config.DefaultRpkProfile()},
		CloudAuths: []config.RpkCloudAuth{config.DefaultRpkCloudAuth()},
	}
	y.CurrentProfile = y.Profiles[0].Name
	y.CurrentCloudAuthOrgID = y.CloudAuths[0].OrgID
	y.CurrentCloudAuthKind = y.CloudAuths[0].Kind
	return &rpkCfg{
		PodContext: p,
		RpkYaml:    &y,
		fixups:     []Fixup{},
	}
}

type rpkCfg struct {
	*PodContext
	*config.RpkYaml
	fixups []Fixup

	// These are created only once, on demand
	concrete map[string]any
}

const (
	RPKProfileYamlFile = "rpk.yaml"
)

// Template currently does not create template, but rather is a concrete snapshot of rpk profile
func (r *rpkCfg) Template(contents map[string]string) error {
	rpkConfig, err := yaml.Marshal(r.RpkYaml)
	if err != nil {
		return fmt.Errorf("could not serialize node config: %w", err)
	}
	contents[RPKProfileYamlFile] = string(rpkConfig)
	if r.fixups == nil {
		r.fixups = []Fixup{}
	}
	fixups, err := json.Marshal(r.fixups)
	if err != nil {
		return fmt.Errorf("could not serialize node fixups: %w", err)
	}
	_ = fixups
	// TODO: Fixup should hold SCRAM or SASL credentials.
	// contents[RedpandaYamlFixupFile] = string(fixups)
	return nil
}
