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
}

const (
	RPKProfileYamlFile      = "rpk.yaml"
	RPKProfileYamlFixupFile = "rpk.yaml.fixups"
)

func (r *rpkCfg) AddFixup(field, cel string) {
	r.fixups = append(r.fixups, Fixup{
		Field: field,
		CEL:   cel,
	})
}

// Template currently does not create template, but rather is a concrete snapshot of rpk profile
func (r *rpkCfg) Template(contents map[string]string) error {
	rpkConfig, err := yaml.Marshal(r.RpkYaml)
	if err != nil {
		return fmt.Errorf("could not serialize rpk profile config: %w", err)
	}
	contents[RPKProfileYamlFile] = string(rpkConfig)

	if r.fixups == nil {
		r.fixups = []Fixup{}
	}
	fixups, err := json.Marshal(r.fixups)
	if err != nil {
		return fmt.Errorf("could not serialize rpk profile fixups: %w", err)
	}
	contents[RPKProfileYamlFixupFile] = string(fixups)

	return nil
}
