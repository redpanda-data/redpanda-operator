// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_servicemonitor.go.tpl
package operator

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

func ServiceMonitor(dot *helmette.Dot) *monitoringv1.ServiceMonitor {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Monitoring.Enabled {
		return nil
	}

	// Render the scheme as lowercase. The prometheus-operator typed constants
	// (monitoringv1.SchemeHTTP / SchemeHTTPS) resolve to "HTTP" / "HTTPS",
	// which older prometheus-operator CRDs reject with
	// `spec.endpoints[0].scheme: Unsupported value`. Lowercase works on every
	// version. See #1511.
	endpoint := monitoringv1.Endpoint{
		Port:   "https",
		Path:   "/metrics",
		Scheme: ptr.To(monitoringv1.Scheme("https")),
		HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
			HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
				TLSConfig: &monitoringv1.TLSConfig{
					SafeTLSConfig: monitoringv1.SafeTLSConfig{
						InsecureSkipVerify: ptr.To(true),
					},
				},
			},
		},
		BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}

	if values.Monitoring.ScrapeInterval != "" {
		endpoint.Interval = monitoringv1.Duration(values.Monitoring.ScrapeInterval)
	}

	// Stamp the source cluster onto every series, for deployments that
	// aggregate several clusters' operator metrics into one backend. See the
	// ClusterLabel godoc for why this is off by default.
	//
	// A relabeling with a targetLabel and a replacement but no sourceLabels is
	// the standard way to set a static label at scrape time; `action` defaults
	// to "replace".
	if values.Monitoring.ClusterLabel.Enabled {
		labelName := values.Monitoring.ClusterLabel.Name
		if labelName == "" {
			labelName = "redpanda_k8s_cluster"
		}

		// Inherit multicluster.name only when multicluster mode is actually
		// enabled. The value is a plain string that can be set with
		// multicluster.enabled false, where it configures nothing else — so
		// falling back to it unconditionally would let a stale or aspirational
		// name silently become a metric label, which is the kind of action at a
		// distance nobody goes looking for.
		labelValue := values.Monitoring.ClusterLabel.Value
		if labelValue == "" && values.Multicluster.Enabled {
			labelValue = values.Multicluster.Name
		}

		// Nothing meaningful to stamp. An empty replacement would produce an
		// empty label, which reads as data rather than as absence, so fail the
		// render instead of shipping it.
		if labelValue == "" {
			panic("monitoring.clusterLabel.value must be set unless multicluster.enabled is true and multicluster.name is non-empty")
		}

		endpoint.RelabelConfigs = append(endpoint.RelabelConfigs, monitoringv1.RelabelConfig{
			TargetLabel: labelName,
			Replacement: ptr.To(labelValue),
		})
	}

	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceMonitor",
			APIVersion: "monitoring.coreos.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cleanForK8sWithSuffix(Fullname(dot), "metrics-monitor"),
			Labels:      helmette.Merge(Labels(dot), values.Monitoring.Labels),
			Namespace:   dot.Release.Namespace,
			Annotations: values.Annotations,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{endpoint},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{dot.Release.Namespace},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: Labels(dot),
			},
		},
	}
}
