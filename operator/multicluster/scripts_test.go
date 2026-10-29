// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

func TestExternalAdvertiseInputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		replicas  int32
		external  *redpandav1alpha2.External
		domain    string
		addresses []string
		errMsg    string
	}{
		{name: "no external", replicas: 3},
		{
			name:     "disabled ignores domain and addresses",
			replicas: 3,
			external: &redpandav1alpha2.External{
				Enabled:   ptr.To(false),
				Domain:    ptr.To("example.com"),
				Addresses: []string{"a", "b", "c"},
			},
		},
		{
			name:     "domain only",
			replicas: 3,
			external: &redpandav1alpha2.External{Enabled: ptr.To(true), Domain: ptr.To("example.com")},
			domain:   "example.com",
		},
		{
			name:     "templated domain",
			replicas: 1,
			external: &redpandav1alpha2.External{Enabled: ptr.To(true), Domain: ptr.To("{{ .Release.Namespace }}.example.com")},
			domain:   "ns.example.com",
		},
		{
			name:      "single address serves every broker",
			replicas:  3,
			external:  &redpandav1alpha2.External{Enabled: ptr.To(true), Addresses: []string{"shared"}},
			addresses: []string{"shared", "shared", "shared"},
		},
		{
			name:     "per-broker addresses",
			replicas: 3,
			external: &redpandav1alpha2.External{
				Enabled:   ptr.To(true),
				Domain:    ptr.To("example.com"),
				Addresses: []string{"a", "b", "c"},
			},
			domain:    "example.com",
			addresses: []string{"a", "b", "c"},
		},
		{
			name:      "fewer addresses than replicas",
			replicas:  3,
			external:  &redpandav1alpha2.External{Enabled: ptr.To(true), Addresses: []string{"a", "b"}},
			addresses: []string{"a", "b"},
		},
		{
			name:     "bad domain template",
			replicas: 1,
			external: &redpandav1alpha2.External{Enabled: ptr.To(true), Domain: ptr.To("{{ .Release.Namespace")},
			errMsg:   "expanding external domain template",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// External is per pool, so a preceding pool must not shift which
			// address a broker gets.
			first := brokerPool("first", 2, nil)
			pool := brokerPool("second", tc.replicas, tc.external)
			pools := []*redpandav1alpha2.RedpandaBrokerPool{first, pool}
			state, err := NewRenderState(nil, stretchCluster(), pools, pools, "test")
			require.NoError(t, err)

			domain, addresses, err := externalAdvertiseInputs(state, pool)
			if tc.errMsg != "" {
				require.ErrorContains(t, err, tc.errMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.domain, domain)
			require.Equal(t, tc.addresses, addresses)
		})
	}
}

func TestConfiguratorShExternalAddress(t *testing.T) {
	base := ScriptParams{
		InternalAdvertiseAddress: "cluster-pool-${POD_ORDINAL}.ns",
		KafkaPort:                9093,
		HTTPPort:                 8082,
	}
	kafka := []ExternalAdvertisedListener{{Name: "default", Port: 30092}}
	http := []ExternalAdvertisedListener{{Name: "default", Port: 30082}}

	// The prologue's BROKER_INDEX line holds a backtick, so each expected
	// script splices it into the raw string.
	prologue := `set -xe
SERVICE_NAME=$1
KUBERNETES_NODE_NAME=$2
POD_ORDINAL=${SERVICE_NAME##*-}
BROKER_INDEX=` + "`expr $POD_ORDINAL + 1`" + `

CONFIG=/etc/redpanda/redpanda.yaml

# Setup config files
cp /tmp/base-config/redpanda.yaml "${CONFIG}"
`

	for _, tc := range []struct {
		name           string
		params         ScriptParams
		expectedScript string
	}{
		{
			name: "no external listeners",
			params: func() ScriptParams {
				p := base
				p.ExternalDomain = "example.com"
				p.ExternalAddresses = []string{"a"}
				return p
			}(),
			expectedScript: prologue + `
LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":9093}"
rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[0] "$LISTENER"

LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":8082}"
rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[0] "$LISTENER"`,
		},
		{
			name: "zero value advertises the pod name",
			params: func() ScriptParams {
				p := base
				p.ExternalKafkaListeners = kafka
				p.ExternalHTTPListeners = http
				return p
			}(),
			expectedScript: prologue + `
LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":9093}"
rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[0] "$LISTENER"

EXTERNAL_ADDRESS="${SERVICE_NAME}"

LISTENER="{\"address\":\"${EXTERNAL_ADDRESS}\",\"name\":\"default\",\"port\":30092}"
rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[1] "$LISTENER"

LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":8082}"
rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[0] "$LISTENER"

LISTENER="{\"address\":\"${EXTERNAL_ADDRESS}\",\"name\":\"default\",\"port\":30082}"
rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[1] "$LISTENER"`,
		},
		{
			name: "domain and addresses",
			params: func() ScriptParams {
				p := base
				p.ExternalKafkaListeners = kafka
				p.ExternalHTTPListeners = http
				p.ExternalDomain = "example.com"
				p.ExternalAddresses = []string{"broker-a", "broker-b"}
				return p
			}(),
			expectedScript: prologue + `
LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":9093}"
rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[0] "$LISTENER"

EXTERNAL_ADDRESS="${SERVICE_NAME}"'.example.com'
case "$POD_ORDINAL" in
0) EXTERNAL_ADDRESS='broker-a.example.com' ;;
1) EXTERNAL_ADDRESS='broker-b.example.com' ;;
esac

LISTENER="{\"address\":\"${EXTERNAL_ADDRESS}\",\"name\":\"default\",\"port\":30092}"
rpk redpanda config --config "$CONFIG" set redpanda.advertised_kafka_api[1] "$LISTENER"

LISTENER="{\"address\":\"cluster-pool-${POD_ORDINAL}.ns\",\"name\":\"internal\",\"port\":8082}"
rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[0] "$LISTENER"

LISTENER="{\"address\":\"${EXTERNAL_ADDRESS}\",\"name\":\"default\",\"port\":30082}"
rpk redpanda config --config "$CONFIG" set pandaproxy.advertised_pandaproxy_api[1] "$LISTENER"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := configuratorSh(tc.params)
			require.Equal(t, tc.expectedScript, script)
		})
	}
}

func stretchCluster() *redpandav1alpha2.StretchCluster {
	return &redpandav1alpha2.StretchCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "ns"},
	}
}

func brokerPool(name string, replicas int32, ext *redpandav1alpha2.External) *redpandav1alpha2.RedpandaBrokerPool {
	return &redpandav1alpha2.RedpandaBrokerPool{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Spec: redpandav1alpha2.BrokerPoolSpec{
			EmbeddedBrokerPoolSpec: redpandav1alpha2.EmbeddedBrokerPoolSpec{
				Replicas: ptr.To(replicas),
				External: ext,
			},
		},
	}
}
