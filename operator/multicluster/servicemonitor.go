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
)

// serviceMonitor returns a ServiceMonitor for the Redpanda cluster.
func serviceMonitor(state *RenderState) *monitoringv1.ServiceMonitor {
	if !state.Spec().Monitoring.IsEnabled() {
		return nil
	}

	mon := state.Spec().Monitoring

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

	if state.Spec().IsAdminTLSEnabled() || mon.TLSConfig != nil {
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
			Name:      state.fullname(),
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
