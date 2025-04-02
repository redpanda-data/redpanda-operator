package clusterconfiguration

import (
	"encoding/json"
	"testing"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestCelPatcher(t *testing.T) {
	redpanda := config.RedpandaYaml{
		Redpanda: config.RedpandaNodeConfig{
			Directory:     "/foo/bar",
			DeveloperMode: true,
		},
		Rpk: config.RpkNodeConfig{
			Tuners: config.RpkNodeTuners{
				TuneNetwork:       true,
				TuneDiskScheduler: false,
			},
		},
		Pandaproxy: nil,
		PandaproxyClient: &config.KafkaClient{
			Brokers: []config.SocketAddress{{
				Address: "0.0.0.0",
				Port:    9092,
			}},
			SASLMechanism: ptr.To("SCRAM-SHA-256"),
			SCRAMUsername: ptr.To("user"),
			SCRAMPassword: nil,
		},
		SchemaRegistry:       nil,
		SchemaRegistryClient: nil,
		Other:                nil,
	}

	environ := map[string]string{
		"env1": "bar",
	}
	factory := StdLibFactory(environ)

	tpl := Template[*config.RedpandaYaml]{
		Content: &redpanda,
		Fixups: []Fixup{
			// {Field: "redpanda", CEL: "true"},
			{Field: "redpanda.data_directory", CEL: `"updated directory"`},
			{Field: "pandaproxy_client.scram_username", CEL: `it + "...blah"`},
			{Field: "pandaproxy_client.scram_password", CEL: `"foo" + envString("env1")`},
			{Field: "redpanda.advertised_rpc_api.address", CEL: `"1.2.3.4"`},
			{Field: "rpk.Tuners.tune_network", CEL: "true"},
			{Field: "rpk.Tuners.tune_disk_scheduler", CEL: "true"},
		},
	}

	errs := tpl.Fixup(factory)
	assert.NoError(t, errs)

	assert.Equal(t, "updated directory", redpanda.Redpanda.Directory)
	assert.Equal(t, "user...blah", *redpanda.PandaproxyClient.SCRAMUsername)
	assert.Equal(t, "foobar", *redpanda.PandaproxyClient.SCRAMPassword)
	assert.Equal(t, "1.2.3.4", redpanda.Redpanda.AdvertisedRPCAPI.Address)
	assert.True(t, redpanda.Rpk.Tuners.TuneNetwork)
	assert.True(t, redpanda.Rpk.Tuners.TuneDiskScheduler)

	buf, err := json.Marshal(tpl.Content)
	assert.NoError(t, err)
	t.Log(string(buf))
}

func TestPatcherErrors(t *testing.T) {
	redpanda := config.RedpandaYaml{
		Redpanda: config.RedpandaNodeConfig{
			Directory:     "/foo/bar",
			DeveloperMode: true,
		},
		Rpk: config.RpkNodeConfig{
			Tuners: config.RpkNodeTuners{
				TuneNetwork:       true,
				TuneDiskScheduler: false,
			},
		},
		Pandaproxy: nil,
		PandaproxyClient: &config.KafkaClient{
			Brokers: []config.SocketAddress{{
				Address: "0.0.0.0",
				Port:    9092,
			}},
			SASLMechanism: ptr.To("SCRAM-SHA-256"),
			SCRAMUsername: ptr.To("user"),
			SCRAMPassword: nil,
		},
		SchemaRegistry:       nil,
		SchemaRegistryClient: nil,
		Other:                nil,
	}

	environ := map[string]string{
		"env1": "bar",
	}
	factory := StdLibFactory(environ)

	tpl := Template[*config.RedpandaYaml]{
		Content: &redpanda,
		Fixups: []Fixup{
			{Field: "redpanda", CEL: "true"},
			{Field: "redpanda.data_directory", CEL: `10`},
			{Field: "pandaproxy_client.scram_username", CEL: `it + 6`},
			{Field: "pandaproxy_client.scram_password", CEL: `"foo" + envString("env2")`},
			{Field: "redpanda.advertised_rpc_api", CEL: `{"address": "0.0.0.0"}`},
			{Field: "schema_registry_client.scram_username", CEL: `envString("env2")`},
		},
	}

	errs := tpl.Fixup(factory).(unwrapper).Unwrap()
	assert.Len(t, errs, 6)
	t.Log("errors:", errs)
}

type unwrapper interface {
	Unwrap() []error
}

func TestMapPatcher(t *testing.T) {
	bootstrapTemplate := map[string]string{
		"v1": "90",
		"v2": `["one","two","three"]`,
	}
	environ := map[string]string{
		"env1": "bar",
		"env2": "baz",
	}
	tpl := Template[map[string]string]{
		Content: bootstrapTemplate,
		Fixups: []Fixup{
			// {Field: "redpanda", CEL: "true"},
			{Field: "v2", CEL: `appendYamlStringArray(it, envString("env1"))`},
			{Field: "v3", CEL: `appendYamlStringArray(it, envString("env2"))`},
		},
	}

	errs := tpl.Fixup(StdLibFactory(environ))
	assert.NoError(t, errs)

	buf, err := json.Marshal(tpl.Content)
	assert.NoError(t, err)
	t.Log(string(buf))
}

func TestMapPatcherErrors(t *testing.T) {
	bootstrapTemplate := map[string]string{
		"v1": "90",
		"v2": `["one","two","three"]`,
	}
	environ := map[string]string{
		"env1": "bar",
		"env2": "baz",
	}
	tpl := Template[map[string]string]{
		Content: bootstrapTemplate,
		Fixups: []Fixup{
			// {Field: "redpanda", CEL: "true"},
			{Field: "v1", CEL: `100`},
			{Field: "v2", CEL: `appendYamlStringArray(it, envString("env1"))`},
			{Field: "v3", CEL: `appendYamlStringArray(it, envString("env2"))`},
		},
	}

	errs := tpl.Fixup(StdLibFactory(environ)).(unwrapper).Unwrap()
	assert.Len(t, errs, 1)
	t.Log("errors:", errs)
}
