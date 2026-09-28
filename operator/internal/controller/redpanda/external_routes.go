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
	"context"
	"fmt"
	"sort"
	"strings"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	redpandachart "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/chart"
	"github.com/redpanda-data/redpanda-operator/operator/internal/statuses"
)

// maxListedRoutes caps how many unaccepted routes the condition message names.
const maxListedRoutes = 5

// routeState is a rendered route's current parentRefs and the status its
// Gateway implementation reports for them.
type routeState struct {
	Kind       string
	Name       string
	Namespace  string
	ParentRefs []gatewayv1.ParentReference
	Parents    []gatewayv1.RouteParentStatus
}

// reconcileExternalRoutes sets ExternalRoutesAccepted from the status of the
// cluster's rendered TCPRoutes, TLSRoutes and ListenerSets. They are owned
// resources, so a status change by the Gateway implementation re-enqueues
// the cluster. It never aborts the reconcile: a read error is reported on the
// condition and retried.
func (r *RedpandaReconciler) reconcileExternalRoutes(ctx context.Context, state *clusterReconciliationState, cluster cluster.Cluster) (ctrl.Result, error) {
	rp := state.cluster.Redpanda
	c := cluster.GetClient()

	var missing []string
	if values, err := rp.GetValues(); err == nil {
		for _, kind := range wantedGatewayKinds(values) {
			if _, err := c.RESTMapper().RESTMapping(schema.GroupKind{Group: gatewayv1.GroupName, Kind: kind}, "v1"); apimeta.IsNoMatchError(err) {
				missing = append(missing, kind)
			}
		}
	}

	opts := []client.ListOption{client.InNamespace(rp.Namespace), client.MatchingLabels(r.LifecycleClient.GetOwnerLabels(state.cluster))}
	var routes []routeState
	var tcp gatewayv1.TCPRouteList
	if err := listIfServed(ctx, c, &tcp, opts...); err != nil {
		state.status.Status.SetExternalRoutesAccepted(statuses.ClusterExternalRoutesAcceptedReasonError, err.Error())
		return ctrl.Result{}, nil
	}
	for i := range tcp.Items {
		rt := &tcp.Items[i]
		routes = append(routes, routeState{Kind: "TCPRoute", Name: rt.Name, Namespace: rt.Namespace, ParentRefs: rt.Spec.ParentRefs, Parents: rt.Status.Parents})
	}
	var tls gatewayv1.TLSRouteList
	if err := listIfServed(ctx, c, &tls, opts...); err != nil {
		state.status.Status.SetExternalRoutesAccepted(statuses.ClusterExternalRoutesAcceptedReasonError, err.Error())
		return ctrl.Result{}, nil
	}
	for i := range tls.Items {
		rt := &tls.Items[i]
		routes = append(routes, routeState{Kind: "TLSRoute", Name: rt.Name, Namespace: rt.Namespace, ParentRefs: rt.Spec.ParentRefs, Parents: rt.Status.Parents})
	}
	var sets gatewayv1.ListenerSetList
	if err := listIfServed(ctx, c, &sets, opts...); err != nil {
		state.status.Status.SetExternalRoutesAccepted(statuses.ClusterExternalRoutesAcceptedReasonError, err.Error())
		return ctrl.Result{}, nil
	}

	reason, message := externalRoutesCondition(routes, sets.Items, missing)
	state.status.Status.SetExternalRoutesAccepted(reason, message)
	return ctrl.Result{}, nil
}

// listIfServed lists into list, treating a kind the API doesn't serve as empty.
func listIfServed(ctx context.Context, c client.Client, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.List(ctx, list, opts...); err != nil && !apimeta.IsNoMatchError(err) {
		return err
	}
	return nil
}

// wantedGatewayKinds returns the Gateway API kinds the cluster's values render.
func wantedGatewayKinds(values redpandachart.Values) []string {
	if !values.External.IsGatewayEnabled() {
		return nil
	}
	var tcp, tls bool
	note := func(enabled bool, isTCP, isTLS bool) {
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

// externalRoutesCondition decides ExternalRoutesAccepted. A route is accepted
// when any of its current parentRefs reports Accepted=True; status for a parent
// the route no longer references is ignored, since implementations can leave
// it behind. The message is sorted and capped so it only changes when the
// routes' state does.
func externalRoutesCondition(routes []routeState, sets []gatewayv1.ListenerSet, missing []string) (statuses.ClusterExternalRoutesAcceptedCondition, string) {
	if len(missing) > 0 {
		sort.Strings(missing)
		return statuses.ClusterExternalRoutesAcceptedReasonAPIMissing, fmt.Sprintf("Gateway API %s (%s/v1) not served by the cluster; the external listeners that need it render nothing", strings.Join(missing, ", "), gatewayv1.GroupName)
	}

	var problems []string
	for _, rt := range routes {
		if accepted, why := routeAccepted(rt); !accepted {
			problems = append(problems, fmt.Sprintf("%s %s (%s)", rt.Kind, rt.Name, why))
		}
	}
	for i := range sets {
		if cond := apimeta.FindStatusCondition(sets[i].Status.Conditions, string(gatewayv1.ListenerSetConditionAccepted)); cond == nil || cond.Status != metav1.ConditionTrue {
			problems = append(problems, fmt.Sprintf("ListenerSet %s (%s)", sets[i].Name, conditionReason(cond)))
		}
	}

	if len(problems) == 0 {
		if len(routes) == 0 {
			return statuses.ClusterExternalRoutesAcceptedReasonAccepted, "No Gateway API routes"
		}
		return statuses.ClusterExternalRoutesAcceptedReasonAccepted, fmt.Sprintf("All %d Gateway API routes are accepted", len(routes))
	}
	sort.Strings(problems)
	listed := problems
	if len(listed) > maxListedRoutes {
		listed = append(listed[:maxListedRoutes:maxListedRoutes], fmt.Sprintf("and %d more", len(problems)-maxListedRoutes))
	}
	return statuses.ClusterExternalRoutesAcceptedReasonNotAccepted, fmt.Sprintf("%d not accepted: %s", len(problems), strings.Join(listed, "; "))
}

// routeAccepted reports whether any current parent accepts rt, and otherwise
// why not: the first non-accepted reason, or Pending when no current parent
// has status yet.
func routeAccepted(rt routeState) (bool, string) {
	why := "Pending"
	for _, ref := range rt.ParentRefs {
		for _, p := range rt.Parents {
			if !sameParent(ref, p.ParentRef, rt.Namespace) {
				continue
			}
			cond := apimeta.FindStatusCondition(p.Conditions, string(gatewayv1.RouteConditionAccepted))
			if cond != nil && cond.Status == metav1.ConditionTrue {
				return true, ""
			}
			if why == "Pending" {
				why = conditionReason(cond)
			}
		}
	}
	return false, why
}

func conditionReason(cond *metav1.Condition) string {
	if cond == nil {
		return "Pending"
	}
	return cond.Reason
}

// sameParent compares parent references with the API's defaults applied.
func sameParent(a, b gatewayv1.ParentReference, namespace string) bool {
	group := func(r gatewayv1.ParentReference) gatewayv1.Group {
		return ptr.Deref(r.Group, gatewayv1.Group(gatewayv1.GroupName))
	}
	kind := func(r gatewayv1.ParentReference) gatewayv1.Kind { return ptr.Deref(r.Kind, gatewayv1.Kind("Gateway")) }
	ns := func(r gatewayv1.ParentReference) gatewayv1.Namespace {
		return ptr.Deref(r.Namespace, gatewayv1.Namespace(namespace))
	}
	return group(a) == group(b) && kind(a) == kind(b) && ns(a) == ns(b) && a.Name == b.Name &&
		ptr.Deref(a.SectionName, "") == ptr.Deref(b.SectionName, "") && ptr.Deref(a.Port, 0) == ptr.Deref(b.Port, 0)
}
