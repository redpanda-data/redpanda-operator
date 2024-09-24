// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package configurator

import (
	"testing"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestPopulateRack(t *testing.T) {
	cfg := config.ProdDefault()
	tests := []struct {
		Zone         string
		ZoneID       string
		ExpectedRack string
	}{
		{Zone: "", ZoneID: "", ExpectedRack: ""},
		{Zone: "zone", ZoneID: "", ExpectedRack: "zone"},
		{Zone: "", ZoneID: "zoneid", ExpectedRack: "zoneid"},
		{Zone: "zone", ZoneID: "zoneid", ExpectedRack: "zoneid"},
	}
	for _, tt := range tests {
		populateRack(cfg, tt.Zone, tt.ZoneID)
		assert.Equal(t, tt.ExpectedRack, cfg.Redpanda.Rack)
	}
}

func TestAdditionalListeners(t *testing.T) { //nolint
	sasl := "sasl"
	httpBasic := "http_basic"
	tests := []struct {
		name                            string
		addtionalListenersCfg           string
		hostIndex                       int
		hostIP                          string
		nodeCfg                         config.RedpandaYaml
		expectedKafkaAPI                []config.NamedAuthNSocketAddress
		expectedAdvertisedKafkaAPI      []config.NamedSocketAddress
		expectedKafkaAPITLS             []config.ServerTLS
		expectedPandaProxyAPI           []config.NamedAuthNSocketAddress
		expectedadvertisedPandaProxyAPI []config.NamedSocketAddress
		expectedPandaProxyTLS           []config.ServerTLS
		expectedError                   bool
	}{
		{
			name:                  "invalid listener configuration",
			addtionalListenersCfg: `{"redpanda.advertised_kafka_api":"[{'invalid format'`,
			hostIndex:             1,
			hostIP:                "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{},
			},
			expectedError: true,
		},
		{
			name:                  "no additional listener with empty string",
			addtionalListenersCfg: "",
			hostIndex:             1,
			hostIP:                "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{
					KafkaAPI: []config.NamedAuthNSocketAddress{{
						Name:    "internal",
						Address: "0.0.0.0",
						Port:    9092,
						AuthN:   &sasl,
					}},
				},
			},
			expectedKafkaAPI: []config.NamedAuthNSocketAddress{
				{
					Name:    "internal",
					Address: "0.0.0.0",
					Port:    9092,
					AuthN:   &sasl,
				},
			},
		},
		{
			name:                  "no additional listener {}",
			addtionalListenersCfg: "{}",
			hostIndex:             1,
			hostIP:                "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{
					AdvertisedKafkaAPI: []config.NamedSocketAddress{{
						Name:    "internal",
						Address: "cluster1.redpanda.svc.cluster.local",
						Port:    9092,
					}},
				},
			},
			expectedAdvertisedKafkaAPI: []config.NamedSocketAddress{
				{
					Name:    "internal",
					Address: "cluster1.redpanda.svc.cluster.local",
					Port:    9092,
				},
			},
		},
		{
			name: "additional kafka listener",
			addtionalListenersCfg: `{"redpanda.advertised_kafka_api":"[{'name': 'private-link-kafka', 'address': '{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 7 }}.redpanda.com', 'port': {{30092 | add .Index}}}]",` +
				`"redpanda.kafka_api":"[{'name': 'private-link-kafka', 'address': '0.0.0.0', 'port': {{30092 | add .Index}}, 'authentication_method': 'sasl'}]"}`,
			hostIndex: 1,
			hostIP:    "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{},
			},
			expectedKafkaAPI: []config.NamedAuthNSocketAddress{
				{
					Address: "0.0.0.0",
					Name:    "private-link-kafka",
					Port:    30092 + 1,
					AuthN:   &sasl,
				},
			},
			expectedAdvertisedKafkaAPI: []config.NamedSocketAddress{
				{
					Address: "1-f415bda0-37d7a80.redpanda.com",
					Name:    "private-link-kafka",
					Port:    30092 + 1,
				},
			},
		},
		{
			name: "additional listeners using the address from the external listerners",
			addtionalListenersCfg: `{"redpanda.advertised_kafka_api":"[{'name': 'private-link-kafka', 'port': {{39002 | add .Index}}}]",` +
				`"redpanda.kafka_api":"[{'name': 'private-link-kafka', 'address': '0.0.0.0', 'port': {{39002 | add .Index}}}]",` +
				`"pandaproxy.advertised_pandaproxy_api":"[{'name': 'private-link-proxy', 'port': {{32082 | add .Index}}}]",` +
				`"pandaproxy.pandaproxy_api":"[{'name': 'private-link-proxy', 'address': '0.0.0.0', 'port': {{32082 | add .Index}}}]"}`,
			hostIndex: 1,
			hostIP:    "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{
					KafkaAPI: []config.NamedAuthNSocketAddress{{
						Name:    "kafka-external",
						Address: "0.0.0.0",
						Port:    9092,
						AuthN:   &sasl,
					}},
					AdvertisedKafkaAPI: []config.NamedSocketAddress{{
						Name:    "kafka-external",
						Address: "kafka.cluster123.redpanda.com",
						Port:    9092,
					}},
				},
				Pandaproxy: &config.Pandaproxy{
					PandaproxyAPI: []config.NamedAuthNSocketAddress{{
						Name:    "proxy-external",
						Address: "0.0.0.0",
						Port:    30082,
						AuthN:   &httpBasic,
					}},
					AdvertisedPandaproxyAPI: []config.NamedSocketAddress{{
						Name:    "proxy-external",
						Address: "proxy.cluster123.redpanda.com",
						Port:    30082,
					}},
				},
			},
			expectedKafkaAPI: []config.NamedAuthNSocketAddress{
				{
					Name:    "kafka-external",
					Address: "0.0.0.0",
					Port:    9092,
					AuthN:   &sasl,
				},
				{
					Name:    "private-link-kafka",
					Address: "0.0.0.0",
					Port:    39002 + 1,
					AuthN:   &sasl,
				},
			},
			expectedAdvertisedKafkaAPI: []config.NamedSocketAddress{
				{
					Name:    "kafka-external",
					Address: "kafka.cluster123.redpanda.com",
					Port:    9092,
				},
				{
					Name:    "private-link-kafka",
					Address: "kafka.cluster123.redpanda.com",
					Port:    39002 + 1,
				},
			},
			expectedPandaProxyAPI: []config.NamedAuthNSocketAddress{
				{
					Name:    "proxy-external",
					Address: "0.0.0.0",
					Port:    30082,
					AuthN:   &httpBasic,
				},
				{
					Name:    "private-link-proxy",
					Address: "0.0.0.0",
					Port:    32082 + 1,
					AuthN:   &httpBasic,
				},
			},
			expectedadvertisedPandaProxyAPI: []config.NamedSocketAddress{
				{
					Name:    "proxy-external",
					Address: "proxy.cluster123.redpanda.com",
					Port:    30082,
				},
				{
					Name:    "private-link-proxy",
					Address: "proxy.cluster123.redpanda.com",
					Port:    32082 + 1,
				},
			},
		},
		{
			name: "additional kafka and proxy listeners with mTLS",
			addtionalListenersCfg: `{"redpanda.advertised_kafka_api":"[{'name': 'private-link-kafka', 'address': '{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 7 }}.redpanda.com', 'port': {{39002 | add .Index}}}]",` +
				`"redpanda.kafka_api":"[{'name': 'private-link-kafka', 'address': '0.0.0.0', 'port': {{39002 | add .Index}}, 'authentication_method': 'sasl'}]",` +
				`"pandaproxy.advertised_pandaproxy_api":"[{'name': 'private-link-proxy', 'address': '{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 7 }}.redpanda.com', 'port': {{30282 | add .Index}}}]",` +
				`"pandaproxy.pandaproxy_api":"[{'name': 'private-link-proxy', 'address': '0.0.0.0', 'port': {{30282 | add .Index}}, 'authentication_method': 'sasl'}]"}`,
			hostIndex: 1,
			hostIP:    "192.168.0.1",
			nodeCfg: config.RedpandaYaml{
				Redpanda: config.RedpandaNodeConfig{
					KafkaAPI: []config.NamedAuthNSocketAddress{{
						Name:    "kafka-internal",
						Address: "0.0.0.0",
						Port:    9092,
						AuthN:   &sasl,
					}},
					AdvertisedKafkaAPI: []config.NamedSocketAddress{{
						Name:    "kafka-internal",
						Address: "cluster1.redpanda.svc.cluster.local",
						Port:    9092,
					}},
					KafkaAPITLS: []config.ServerTLS{{
						Name:     resources.ExternalListenerName,
						CertFile: "crt1.pem",
						KeyFile:  "key1.pem",
					}},
				},
				Pandaproxy: &config.Pandaproxy{
					PandaproxyAPI: []config.NamedAuthNSocketAddress{{
						Name:    "proxy-internal",
						Address: "0.0.0.0",
						Port:    30082,
						AuthN:   &sasl,
					}},
					AdvertisedPandaproxyAPI: []config.NamedSocketAddress{{
						Name:    "proxy-internal",
						Address: "cluster1.redpanda.svc.cluster.local",
						Port:    30082,
					}},
					PandaproxyAPITLS: []config.ServerTLS{{
						Name:     resources.PandaproxyPortExternalName,
						CertFile: "crt2.pem",
						KeyFile:  "key2.pem",
					}},
				},
			},
			expectedKafkaAPI: []config.NamedAuthNSocketAddress{
				{
					Name:    "kafka-internal",
					Address: "0.0.0.0",
					Port:    9092,
					AuthN:   &sasl,
				},
				{
					Address: "0.0.0.0",
					Name:    "private-link-kafka",
					Port:    39002 + 1,
					AuthN:   &sasl,
				},
			},
			expectedAdvertisedKafkaAPI: []config.NamedSocketAddress{
				{
					Name:    "kafka-internal",
					Address: "cluster1.redpanda.svc.cluster.local",
					Port:    9092,
				},
				{
					Address: "1-f415bda0-37d7a80.redpanda.com",
					Name:    "private-link-kafka",
					Port:    39002 + 1,
				},
			},
			expectedKafkaAPITLS: []config.ServerTLS{
				{
					Name:     resources.ExternalListenerName,
					CertFile: "crt1.pem",
					KeyFile:  "key1.pem",
				},
				{
					Name:     "private-link-kafka",
					CertFile: "crt1.pem",
					KeyFile:  "key1.pem",
				},
			},
			expectedPandaProxyAPI: []config.NamedAuthNSocketAddress{
				{
					Name:    "proxy-internal",
					Address: "0.0.0.0",
					Port:    30082,
					AuthN:   &sasl,
				},
				{
					Address: "0.0.0.0",
					Name:    "private-link-proxy",
					Port:    30282 + 1,
					AuthN:   &sasl,
				},
			},
			expectedadvertisedPandaProxyAPI: []config.NamedSocketAddress{
				{
					Name:    "proxy-internal",
					Address: "cluster1.redpanda.svc.cluster.local",
					Port:    30082,
				},
				{
					Address: "1-f415bda0-37d7a80.redpanda.com",
					Name:    "private-link-proxy",
					Port:    30282 + 1,
				},
			},
			expectedPandaProxyTLS: []config.ServerTLS{
				{
					Name:     resources.PandaproxyPortExternalName,
					CertFile: "crt2.pem",
					KeyFile:  "key2.pem",
				},
				{
					Name:     "private-link-proxy",
					CertFile: "crt2.pem",
					KeyFile:  "key2.pem",
				},
			},
		},
	}
	for i := 0; i < len(tests); i++ {
		tt := &tests[i]
		err := setAdditionalListeners(tt.addtionalListenersCfg, tt.hostIP, tt.hostIndex, &tt.nodeCfg)
		if tt.expectedError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			validateListenerConfig(t, tt.expectedKafkaAPI, tt.nodeCfg.Redpanda.KafkaAPI, func(c config.NamedAuthNSocketAddress) string { return c.Name })
			validateListenerConfig(t, tt.expectedAdvertisedKafkaAPI, tt.nodeCfg.Redpanda.AdvertisedKafkaAPI, func(c config.NamedSocketAddress) string { return c.Name })
			validateListenerConfig(t, tt.expectedKafkaAPITLS, tt.nodeCfg.Redpanda.KafkaAPITLS, func(c config.ServerTLS) string { return c.Name })
			if len(tt.expectedPandaProxyAPI) > 0 {
				validateListenerConfig(t, tt.expectedPandaProxyAPI, tt.nodeCfg.Pandaproxy.PandaproxyAPI, func(c config.NamedAuthNSocketAddress) string { return c.Name })
			}
			if len(tt.expectedadvertisedPandaProxyAPI) > 0 {
				validateListenerConfig(t, tt.expectedadvertisedPandaProxyAPI, tt.nodeCfg.Pandaproxy.AdvertisedPandaproxyAPI, func(c config.NamedSocketAddress) string { return c.Name })
			}
			if len(tt.expectedPandaProxyTLS) > 0 {
				validateListenerConfig(t, tt.expectedPandaProxyTLS, tt.nodeCfg.Pandaproxy.PandaproxyAPITLS, func(c config.ServerTLS) string { return c.Name })
			}
		}
	}
}

// validateListenerConfig checks whether cfg1 contains cfg2.
func validateListenerConfig[V any](t *testing.T, cfg1, cfg2 []V, getName func(V) string) {
	assert.Equal(t, len(cfg1), len(cfg2))

	m := map[string]*V{}
	for i := 0; i < len(cfg1); i++ {
		m[getName(cfg1[i])] = &cfg1[i]
	}

	for _, c := range cfg2 {
		v, found := m[getName(c)]
		assert.True(t, found, "name", getName(c))
		assert.Equal(t, *v, c)
	}
}
