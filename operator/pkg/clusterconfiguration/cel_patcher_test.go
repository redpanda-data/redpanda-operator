package clusterconfiguration

import (
	"context"
	"testing"

	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

const (
	redpandaDir = "/foo/bar"
	bootstrapV1 = "90"
)

func testRedpandaYaml() config.RedpandaYaml {
	return config.RedpandaYaml{
		Redpanda: config.RedpandaNodeConfig{
			Directory:     redpandaDir,
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
}

func bootstrapTemplate() any {
	return map[string]string{
		"v1": bootstrapV1,
		"v2": `["one","two","three"]`,
	}
}

type testCase struct {
	name     string
	input    func() any
	fixups   []Fixup
	check    func(*testing.T, any)
	errors   []string
	warnings []string
}

var tests = []testCase{
	{
		name: "errors are surfaced",
		input: func() any {
			return ptr.To(testRedpandaYaml())
		},
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `makeError("my error")`},
			{Field: "redpanda.rpc_server.nonexistent.field", CEL: `""`},
		},
		errors: []string{"my error", `nonexistent.field" is not valid`},
	},
	{
		name:  "errorToWarning on non-error value",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `errorToWarning("not an error")`},
		},
		check: func(t *testing.T, out any) {
			assert.Equal(t, "not an error", out.(*config.RedpandaYaml).Redpanda.Directory)
		},
	},
	{
		name:  "errortoWarning on direct error",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `errorToWarning(makeError("wrapped error"))`},
		},
		check:    redpandaDirUnchanged,
		warnings: []string{"wrapped error"},
	},
	{
		name:  "errorToWarning on nested error",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `errorToWarning(repr(makeError("wrapped nested error")))`},
		},
		check:    redpandaDirUnchanged,
		warnings: []string{"wrapped nested error"},
	},
	{
		name:  "nested errorToWarning",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `repr(errorToWarning(makeError("nested wrapped error")))`},
		},
		check:    redpandaDirUnchanged,
		warnings: []string{"nested wrapped error"},
	},
	{
		name:  "working RedpandaYaml patch",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda.data_directory", CEL: `"updated directory"`},
			{Field: "pandaproxy_client.scram_username", CEL: `it + "...blah"`},
			{Field: "pandaproxy_client.scram_password", CEL: `"foo" + envString("env1")`},
			{Field: "redpanda.advertised_rpc_api.address", CEL: `"1.2.3.4"`},
			{Field: "rpk.Tuners.tune_network", CEL: "true"},
			{Field: "rpk.tune_disk_scheduler", CEL: "true"}, // inline member
		},
		check: func(t *testing.T, out any) {
			rpy := out.(*config.RedpandaYaml)
			assert.Equal(t, "updated directory", rpy.Redpanda.Directory)
			assert.Equal(t, "user...blah", *rpy.PandaproxyClient.SCRAMUsername)
			assert.Equal(t, "foobar", *rpy.PandaproxyClient.SCRAMPassword)
			assert.Equal(t, "1.2.3.4", rpy.Redpanda.AdvertisedRPCAPI.Address)
			assert.True(t, rpy.Rpk.Tuners.TuneNetwork)
			assert.True(t, rpy.Rpk.Tuners.TuneDiskScheduler)
		},
	},
	{
		name:  "redpandaYaml patching errors",
		input: func() any { return ptr.To(testRedpandaYaml()) },
		fixups: []Fixup{
			{Field: "redpanda", CEL: "true"},
			{Field: "redpanda.data_directory", CEL: `10`},
			{Field: "pandaproxy_client.scram_username", CEL: `it + 6`},
			{Field: "pandaproxy_client.scram_password", CEL: `"foo" + envString("noEnv")`},
			{Field: "redpanda.advertised_rpc_api", CEL: `{"address": "0.0.0.0"}`}, // this one could be fixed if required
			{Field: "schema_registry_client.scram_username", CEL: `envString("noEnv")`},
		},
		errors: []string{
			`cannot assign field "redpanda"`,
			`result of type int64 is not compatible with field`,
			`no such overload`,
			`not found in environment`,
			`not compatible with field`, // this one could be fixed if required
			`not found in environment`,
		},
	},
	{
		name:  "patching a map",
		input: bootstrapTemplate,
		fixups: []Fixup{
			{Field: "v2", CEL: `appendYamlStringArray(it, envString("env1"))`},
			{Field: "v3", CEL: `appendYamlStringArray(it, envString("env2"))`},
		},
		check: func(t *testing.T, out any) {
			m := out.(map[string]string)
			assert.Equal(t, bootstrapV1, m["v1"])
			assert.Equal(t, `["one","two","three","bar"]`, m["v2"])
			assert.Equal(t, `["baz"]`, m["v3"])
		},
	},
	{
		name:  "map patching errors",
		input: bootstrapTemplate,
		fixups: []Fixup{
			{Field: "v1", CEL: `100`},
		},
		errors: []string{`cannot assign field "v1": result of type int64`},
	},
}

func redpandaDirUnchanged(t *testing.T, out any) {
	assert.Equal(t, redpandaDir, out.(*config.RedpandaYaml).Redpanda.Directory)
}

func TestCelPatching(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inp := tc.input()
			tpl := Template[any]{
				Content: inp,
				Fixups:  tc.fixups,
			}
			environ := map[string]string{
				"env1": "bar",
				"env2": "baz",
			}
			factory := StdLibFactory(context.TODO(), environ, nil)
			err := tpl.Fixup(factory)
			if tc.check != nil {
				tc.check(t, tpl.Content)
			}
			var errs []error
			if unw, ok := err.(unwrapper); ok {
				errs = unw.Unwrap()
			}
			assert.Len(t, errs, len(tc.errors), "number of errors reported")
			for i, exp := range tc.errors {
				assert.Contains(t, errs[i].Error(), exp, "error mismatch")
			}
			assert.Len(t, tpl.Warnings, len(tc.warnings), "number of warnings reported")
			for i, exp := range tc.warnings {
				assert.Contains(t, tpl.Warnings[i].Error(), exp, "warning mismatch")
			}
		})
	}
}

type unwrapper interface {
	Unwrap() []error
}
