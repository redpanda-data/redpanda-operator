// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package console

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
	endpoint := monitoringv1.Endpoint{
		Interval: state.Values.Monitoring.ScrapeInterval,
		Path:     "/admin/metrics",
		Port:     servicePortName,
		Scheme:   ptr.To(monitoringv1.SchemeHTTP),
	}
	tlsCertFilepath := helmette.Dig(state.Values.Config, "", "server", "tls", "certFilepath")
	tlsKeyFilepath := helmette.Dig(state.Values.Config, "", "server", "tls", "keyFilepath")
	tlsEnabled := helmette.Dig(state.Values.Config, false, "server", "tls", "enabled")
	if tlsEnabled.(bool) && tlsCertFilepath != "" && tlsKeyFilepath != "" {
		tlsConfig := &monitoringv1.TLSConfig{
			TLSFilesConfig: monitoringv1.TLSFilesConfig{
				CertFile: tlsCertFilepath.(string),
				KeyFile:  tlsKeyFilepath.(string),
			},
		}
		endpoint.TLSConfig = tlsConfig
		endpoint.Scheme = ptr.To(monitoringv1.SchemeHTTPS)
	}

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       monitoringv1.ServiceMonitorsKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.FullName(),
			Namespace: state.Namespace,
			Labels:    helmette.Merge(state.Labels(nil), state.Values.Monitoring.Labels),
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{endpoint},
			Selector: metav1.LabelSelector{
				MatchLabels: state.Labels(nil),
			},
		},
	}
}
