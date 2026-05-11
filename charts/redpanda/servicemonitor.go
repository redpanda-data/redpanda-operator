// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_servicemonitor.go.tpl
package redpanda

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func ServiceMonitor(state *RenderState) *monitoringv1.ServiceMonitor {
	if !state.Values.Monitoring.Enabled {
		return nil
	}

	// Render the scheme as lowercase. The prometheus-operator typed constants
	// (monitoringv1.SchemeHTTP / SchemeHTTPS) resolve to "HTTP" / "HTTPS" —
	// gotohelm faithfully emits those values, but older prometheus-operator
	// CRDs only accept "http" / "https" in the Scheme enum and reject the
	// rendered ServiceMonitor at apply time. Lowercase is accepted by every
	// CRD version we support, so produce it explicitly via a string-typed
	// conversion rather than the upstream constants. See #1511.
	endpoint := monitoringv1.Endpoint{
		Interval: state.Values.Monitoring.ScrapeInterval,
		Path:     "/public_metrics",
		Port:     "admin",
		Scheme:   ptr.To(monitoringv1.Scheme("http")),
	}

	if state.Values.Listeners.Admin.TLS.IsEnabled(&state.Values.TLS) || state.Values.Monitoring.TLSConfig != nil {
		endpoint.Scheme = ptr.To(monitoringv1.Scheme("https"))
		endpoint.TLSConfig = state.Values.Monitoring.TLSConfig

		if endpoint.TLSConfig == nil {
			endpoint.TLSConfig = &monitoringv1.TLSConfig{
				SafeTLSConfig: monitoringv1.SafeTLSConfig{
					InsecureSkipVerify: ptr.To(true),
				},
			}
		}
	}

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       monitoringv1.ServiceMonitorsKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      Fullname(state),
			Namespace: state.Release.Namespace,
			Labels:    helmette.Merge(FullLabels(state), state.Values.Monitoring.Labels),
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"monitoring.redpanda.com/enabled": "true",
					"app.kubernetes.io/name":          Name(state),
					"app.kubernetes.io/instance":      state.Release.Name,
				},
			},
		},
	}
}
