// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package resources

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// The pod lifecycle hooks curl the admin API at a host derived from
// --cluster-domain. Nothing else covers the emitted script: there is no golden
// for it, and kuttl asserts nothing on the lifecycle secret. The rows on the
// default domain are the regression guard - that output has to stay
// byte-identical to the URL these hooks hardcoded before the domain was
// threaded through.
func TestPreStartStopScriptCURLCommands(t *testing.T) {
	for name, tc := range map[string]struct {
		clusterDomain       string
		trailingDotDisabled bool
		tls                 vectorizedv1alpha1.AdminAPITLS
		wantNodeID          string
		wantMaintenance     string
	}{
		"default domain, plaintext": {
			clusterDomain:   "cluster.local",
			wantNodeID:      "curl -s http://${POD_NAME}.test.default.svc.cluster.local:9644/v1/node_config",
			wantMaintenance: "curl -s http://${POD_NAME}.test.default.svc.cluster.local:9644/v1/brokers/${NODE_ID}/maintenance",
		},
		"default domain, TLS": {
			clusterDomain:   "cluster.local",
			tls:             vectorizedv1alpha1.AdminAPITLS{Enabled: true},
			wantNodeID:      "curl -s --cacert /etc/tls/certs/admin/tls.crt https://${POD_NAME}.test.default.svc.cluster.local:9644/v1/node_config",
			wantMaintenance: "curl -s --cacert /etc/tls/certs/admin/tls.crt https://${POD_NAME}.test.default.svc.cluster.local:9644/v1/brokers/${NODE_ID}/maintenance",
		},
		"default domain, mutual TLS": {
			clusterDomain:   "cluster.local",
			tls:             vectorizedv1alpha1.AdminAPITLS{Enabled: true, RequireClientAuth: true},
			wantNodeID:      "curl -s --cacert /etc/tls/certs/admin/ca/ca.crt --cert /etc/tls/certs/admin/tls.crt --key /etc/tls/certs/admin/tls.key https://${POD_NAME}.test.default.svc.cluster.local:9644/v1/node_config",
			wantMaintenance: "curl -s --cacert /etc/tls/certs/admin/ca/ca.crt --cert /etc/tls/certs/admin/tls.crt --key /etc/tls/certs/admin/tls.key https://${POD_NAME}.test.default.svc.cluster.local:9644/v1/brokers/${NODE_ID}/maintenance",
		},
		// The whole point of the change: the host follows --cluster-domain, so
		// it resolves and matches the SANs the V1 controller mints from the
		// same value.
		"custom domain, plaintext": {
			clusterDomain:   "k8s.example",
			wantNodeID:      "curl -s http://${POD_NAME}.test.default.svc.k8s.example:9644/v1/node_config",
			wantMaintenance: "curl -s http://${POD_NAME}.test.default.svc.k8s.example:9644/v1/brokers/${NODE_ID}/maintenance",
		},
		"custom domain, TLS": {
			clusterDomain:   "k8s.example",
			tls:             vectorizedv1alpha1.AdminAPITLS{Enabled: true},
			wantNodeID:      "curl -s --cacert /etc/tls/certs/admin/tls.crt https://${POD_NAME}.test.default.svc.k8s.example:9644/v1/node_config",
			wantMaintenance: "curl -s --cacert /etc/tls/certs/admin/tls.crt https://${POD_NAME}.test.default.svc.k8s.example:9644/v1/brokers/${NODE_ID}/maintenance",
		},
		// HeadlessServiceFQDN emits no trailing dot here, so adminAPIHost has
		// nothing to trim and must not chew into the domain.
		"trailing dot disabled": {
			clusterDomain:       "k8s.example",
			trailingDotDisabled: true,
			wantNodeID:          "curl -s http://${POD_NAME}.test.default.svc.k8s.example:9644/v1/node_config",
			wantMaintenance:     "curl -s http://${POD_NAME}.test.default.svc.k8s.example:9644/v1/brokers/${NODE_ID}/maintenance",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cluster := &vectorizedv1alpha1.Cluster{
				Spec: vectorizedv1alpha1.ClusterSpec{
					DNSTrailingDotDisabled: tc.trailingDotDisabled,
					Configuration: vectorizedv1alpha1.RedpandaConfig{
						AdminAPI: []vectorizedv1alpha1.AdminAPI{{Port: 9644, TLS: tc.tls}},
					},
				},
			}
			cluster.Name = "test"
			cluster.Namespace = "default"

			// Go through HeadlessServiceFQDN rather than a literal so the test
			// covers the same domain-to-FQDN step the controller takes.
			fqdn := NewHeadlessService(nil, cluster, nil, nil, logr.Discard()).HeadlessServiceFQDN(tc.clusterDomain)
			res := PreStartStopScriptSecret(nil, cluster, nil, fqdn, types.NamespacedName{}, types.NamespacedName{}, logr.Discard())

			require.Equal(t, tc.wantNodeID, res.composeCURLGetNodeIDCommand("-s"))
			require.Equal(t, tc.wantMaintenance, res.composeCURLMaintenanceCommand("-s", nil))
		})
	}
}
