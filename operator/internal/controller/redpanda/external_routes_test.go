// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package redpanda

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

func TestExternalRoutesCondition(t *testing.T) {
	gwPort := func(port int32) gatewayv1.ParentReference {
		return gatewayv1.ParentReference{Name: "gw", Namespace: ptr.To(gatewayv1.Namespace("infra")), Port: ptr.To(gatewayv1.PortNumber(port))}
	}
	ls := func(section string) gatewayv1.ParentReference {
		return gatewayv1.ParentReference{Group: ptr.To(gatewayv1.Group(gatewayv1.GroupName)), Kind: ptr.To(gatewayv1.Kind("ListenerSet")), Name: "rp-infra-gw", SectionName: ptr.To(gatewayv1.SectionName(section))}
	}
	status := func(ref gatewayv1.ParentReference, accepted bool, reason string) gatewayv1.RouteParentStatus {
		s := metav1.ConditionFalse
		if accepted {
			s = metav1.ConditionTrue
		}
		return gatewayv1.RouteParentStatus{ParentRef: ref, Conditions: []metav1.Condition{{Type: "Accepted", Status: s, Reason: reason}}}
	}
	route := func(name string, refs []gatewayv1.ParentReference, parents ...gatewayv1.RouteParentStatus) routeState {
		return routeState{Kind: "TCPRoute", Name: name, Namespace: "redpanda", ParentRefs: refs, Parents: parents}
	}
	listenerSet := func(accepted bool, reason string) gatewayv1.ListenerSet {
		s := metav1.ConditionFalse
		if accepted {
			s = metav1.ConditionTrue
		}
		return gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Name: "rp-infra-gw"}, Status: gatewayv1.ListenerSetStatus{Conditions: []metav1.Condition{{Type: "Accepted", Status: s, Reason: reason}}}}
	}

	for _, tc := range []struct {
		name    string
		routes  []routeState
		sets    []gatewayv1.ListenerSet
		missing []string
		reason  statuses.ClusterExternalRoutesAcceptedCondition
		message string
	}{
		{name: "no routes", reason: statuses.ClusterExternalRoutesAcceptedReasonAccepted, message: "No Gateway API routes"},
		{
			name:    "all accepted",
			routes:  []routeState{route("rp-0", []gatewayv1.ParentReference{gwPort(9200)}, status(gwPort(9200), true, "Accepted"))},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonAccepted,
			message: "All 1 Gateway API routes are accepted",
		},
		{
			// A broker scaled past the Gateway's pre-provisioned ports.
			name:    "no matching parent",
			routes:  []routeState{route("rp-4", []gatewayv1.ParentReference{gwPort(9204)}, status(gwPort(9204), false, "NoMatchingParent"))},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonNotAccepted,
			message: "1 not accepted: TCPRoute rp-4 (NoMatchingParent)",
		},
		{
			// Dual-attach after the Gateway's own listener is gone.
			name:    "accepted by one of two parents",
			routes:  []routeState{route("rp-0", []gatewayv1.ParentReference{gwPort(9200), ls("kafka-default-0")}, status(gwPort(9200), false, "NoMatchingParent"), status(ls("kafka-default-0"), true, "Accepted"))},
			sets:    []gatewayv1.ListenerSet{listenerSet(true, "Accepted")},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonAccepted,
			message: "All 1 Gateway API routes are accepted",
		},
		{
			// Implementations can leave status for a parent the route no longer references.
			name:    "stale parent status is ignored",
			routes:  []routeState{route("rp-0", []gatewayv1.ParentReference{ls("kafka-default-0")}, status(gwPort(9200), true, "Accepted"))},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonNotAccepted,
			message: "1 not accepted: TCPRoute rp-0 (Pending)",
		},
		{
			name:    "listenerset not allowed",
			routes:  []routeState{route("rp-0", []gatewayv1.ParentReference{gwPort(9200), ls("kafka-default-0")}, status(gwPort(9200), true, "Accepted"))},
			sets:    []gatewayv1.ListenerSet{listenerSet(false, "NotAllowed")},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonNotAccepted,
			message: "1 not accepted: ListenerSet rp-infra-gw (NotAllowed)",
		},
		{
			name:    "api missing",
			routes:  nil,
			missing: []string{"TCPRoute"},
			reason:  statuses.ClusterExternalRoutesAcceptedReasonAPIMissing,
			message: "Gateway API TCPRoute (gateway.networking.k8s.io/v1) not served by the cluster; the external listeners that need it render nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason, message := externalRoutesCondition(tc.routes, tc.sets, tc.missing)
			require.Equal(t, tc.reason, reason)
			require.Equal(t, tc.message, message)
		})
	}

	// The message is capped and order-independent, so it changes only when the
	// routes' state does and never churns the status.
	var routes, reversed []routeState
	for i := range 7 {
		r := route(fmt.Sprintf("rp-%d", i), []gatewayv1.ParentReference{gwPort(int32(9200 + i))}, status(gwPort(int32(9200+i)), false, "NoMatchingParent"))
		routes = append(routes, r)
		reversed = append([]routeState{r}, reversed...)
	}
	_, a := externalRoutesCondition(routes, nil, nil)
	_, b := externalRoutesCondition(reversed, nil, nil)
	require.Equal(t, a, b)
	require.Equal(t, "7 not accepted: TCPRoute rp-0 (NoMatchingParent); TCPRoute rp-1 (NoMatchingParent); TCPRoute rp-2 (NoMatchingParent); TCPRoute rp-3 (NoMatchingParent); TCPRoute rp-4 (NoMatchingParent); and 2 more", a)
}
