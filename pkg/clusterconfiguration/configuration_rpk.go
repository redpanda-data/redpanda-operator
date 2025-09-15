package clusterconfiguration

import (
	"encoding/json"
	"fmt"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	RPKProfileYamlFile      = "rpk.yaml"
	RPKProfileYamlFixupFile = "rpk.yaml.fixups"

	// currentRPKVersion is a lowest denominator of the following versions
	// https://github.com/redpanda-data/redpanda/blob/v24.1.1/src/go/rpk/pkg/config/params.go#L88
	// https://github.com/redpanda-data/redpanda/blob/v24.2.5/src/go/rpk/pkg/config/params.go#L88
	// https://github.com/redpanda-data/redpanda/blob/v24.3.14/src/go/rpk/pkg/config/params.go#L89
	// https://github.com/redpanda-data/redpanda/blob/v25.1.4/src/go/rpk/pkg/config/params.go#L89
	// which is used in default rpk configuration in:
	// https://github.com/redpanda-data/redpanda/blob/c48fbed51193a35aaf42fb3a6b047da612dde421/src/go/rpk/pkg/config/rpk_yaml.go#L41
	// Operator just follows the default rpk configuration as much as possible.
	// When RPK starts Redpanda process it reloads the RPK profile which changes/correct the version.
	currentRPKVersion = 4
)

func NewRPKCfg(p *PodContext) *rpkCfg {
	y := config.RpkYaml{
		Version:    currentRPKVersion,
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
