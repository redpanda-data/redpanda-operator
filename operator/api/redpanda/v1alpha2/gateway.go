// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"k8s.io/utils/ptr"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
)

// GatewayAPIKinds returns the Gateway API kinds a cluster's rendered values
// produce: TCPRoute and TLSRoute for enabled external listeners of those
// types, and ListenerSet when listenerSet mode carries them.
func GatewayAPIKinds(values redpandachart.Values) []string {
	if !values.External.IsGatewayEnabled() {
		return nil
	}
	var tcp, tls bool
	note := func(enabled, isTCP, isTLS bool) {
		tcp = tcp || (enabled && isTCP)
		tls = tls || (enabled && isTLS)
	}
	for _, l := range values.Listeners.Kafka.External {
		note(ptr.Deref(l.Enabled, values.External.Enabled), l.IsTCPRouteListener(), l.IsTLSRouteListener())
	}
	for _, l := range values.Listeners.HTTP.External {
		note(ptr.Deref(l.Enabled, values.External.Enabled), l.IsTCPRouteListener(), l.IsTLSRouteListener())
	}
	for _, l := range values.Listeners.Admin.External {
		note(ptr.Deref(l.Enabled, values.External.Enabled), l.IsTCPRouteListener(), l.IsTLSRouteListener())
	}
	for _, l := range values.Listeners.SchemaRegistry.External {
		note(ptr.Deref(l.Enabled, values.External.Enabled), l.IsTCPRouteListener(), l.IsTLSRouteListener())
	}
	var kinds []string
	if tcp {
		kinds = append(kinds, "TCPRoute")
	}
	if tls {
		kinds = append(kinds, "TLSRoute")
	}
	if (tcp || tls) && values.External.Gateway.IsListenerSetEnabled() {
		kinds = append(kinds, "ListenerSet")
	}
	return kinds
}
