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
	"encoding/json"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
)

// serviceMonitor returns ServiceMonitors across every local pool with
// monitoring enabled. Wrapper used by RenderResources.
func serviceMonitor(state *RenderState) []*monitoringv1.ServiceMonitor {
	var out []*monitoringv1.ServiceMonitor
	for _, pool := range state.inClusterPools {
		if sm := serviceMonitorForPool(state, pool); sm != nil {
			out = append(out, sm)
		}
	}
	return out
}

// serviceMonitorForPool returns a ServiceMonitor for a single local pool,
// named <cluster>-<pool>. Scrape interval, labels, TLS config come from
// the pool's Monitoring; the admin-TLS check reads the pool's TLS.
func serviceMonitorForPool(state *RenderState, pool *redpandav1alpha2.RedpandaBrokerPool) *monitoringv1.ServiceMonitor {
	if !pool.Spec.Monitoring.IsEnabled() {
		return nil
	}

	mon := pool.Spec.Monitoring

	var interval monitoringv1.Duration
	if mon.ScrapeInterval != nil {
		interval = monitoringv1.Duration(*mon.ScrapeInterval)
	}

	endpoint := monitoringv1.Endpoint{
		Interval: interval,
		Path:     publicMetricsPath,
		Port:     internalAdminAPIPortName,
		Scheme:   ptr.To(monitoringv1.SchemeHTTP),
	}

	if pool.Spec.IsAdminTLSEnabled() || mon.TLSConfig != nil {
		endpoint.Scheme = ptr.To(monitoringv1.SchemeHTTPS)

		// Use custom TLS config if provided, otherwise fall back to insecure skip verify.
		if mon.TLSConfig != nil {
			var tlsConfig monitoringv1.TLSConfig
			if err := json.Unmarshal(mon.TLSConfig.Raw, &tlsConfig); err == nil {
				endpoint.TLSConfig = &tlsConfig
			}
		}
		if endpoint.TLSConfig == nil {
			endpoint.TLSConfig = &monitoringv1.TLSConfig{
				SafeTLSConfig: monitoringv1.SafeTLSConfig{
					InsecureSkipVerify: ptr.To(true),
				},
			}
		}
	}

	labels := state.commonLabels()
	for k, v := range mon.Labels {
		labels[k] = v
	}

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       monitoringv1.ServiceMonitorsKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.poolFullname(pool),
			Namespace: state.namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					labelMonitorKey:  "true",
					labelNameKey:     labelNameValue,
					labelInstanceKey: state.releaseName,
				},
			},
		},
	}
}
