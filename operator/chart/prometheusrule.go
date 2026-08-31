// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// +gotohelm:filename=_prometheusrule.go.tpl
package operator

import (
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/redpanda-data/redpanda-operator/gotohelm/helmette"
)

// PrometheusRule emits the recommended recording rules and alerts for the
// operator's controller-runtime and reconcile-health metrics. Gated on
// values.monitoring.rulesEnabled so consumers can opt in independently of
// the ServiceMonitor.
//
// Returns nil when rulesEnabled is false so the rendered manifest list
// has no entry — same convention as ServiceMonitor.
//
// The rules cover three concerns:
//
//   - Recording rules normalise the verbose
//     `controller_runtime_reconcile_*` metrics into shorter aliases that
//     dashboards and ad-hoc queries can use without typing the full name
//     each time.
//   - Alerts target the failure modes the operator can detect from its own
//     metrics: a controller that has stopped reconciling, a controller
//     spinning at high rate without reaching steady state, sustained
//     reconcile errors.
//   - The alert thresholds are conservative defaults; operators can
//     override by setting their own PrometheusRule alongside this one.
//
// Every expression is scoped to THIS install. controller_runtime_* and
// workqueue_* are not Redpanda-specific metric names — every
// controller-runtime-based operator on the cluster exports them (Karpenter,
// Flux, cert-manager, Trivy, ...). Unscoped, the alerts fire for foreign
// controllers and, worse, `sum by (controller)` merges same-named controllers
// across operators so downstream consumers cannot filter the recorded series
// after the fact. Two labels pin the series:
//
//   - job: prometheus-operator defaults a ServiceMonitor's `job` to the scraped
//     Service's name when spec.jobLabel is unset, which is the case for the
//     ServiceMonitor this chart renders — so `job` is the metrics Service name.
//   - namespace: the release namespace, which the chart always knows.
//
// Both are also kept in the `sum by (...)` grouping so two operator installs on
// one cluster produce distinct recorded series instead of merging. See K8S-926.
//
// The alerts that read a recorded series carry the matcher as well, not only the
// ones reading raw metrics. Each install renders its own PrometheusRule into the
// same Prometheus, and a recorded series is not namespaced to its producer, so an
// unscoped alert expression evaluates against every install's series: two
// installs would each alert on the other's controllers.
//
// The load-shaped alerts are calibrated for the interval-driven CR controllers
// (Topic, User, Schema, ...), which requeue every CR every sync interval: the
// steady-state reconcile rate is proportional to CR count × engaged clusters /
// sync interval, and each controller runs a single worker that legitimately
// sits busy under that load while its queue stays empty. Runaway therefore
// counts only churn (everything but result="requeue_after"), and saturation is
// measured as p99 time-in-queue rather than the sampled busy-worker gauge.
// See K8S-927.
func PrometheusRule(dot *helmette.Dot) *monitoringv1.PrometheusRule {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Monitoring.RulesEnabled {
		return nil
	}

	scope := fmt.Sprintf(`job="%s", namespace="%s"`,
		cleanForK8sWithSuffix(Fullname(dot), "metrics-service"),
		dot.Release.Namespace,
	)

	return &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PrometheusRule",
			APIVersion: "monitoring.coreos.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cleanForK8sWithSuffix(Fullname(dot), "reconcile-health"),
			Labels:      Labels(dot),
			Namespace:   dot.Release.Namespace,
			Annotations: values.Annotations,
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "redpanda-operator.controller-runtime.recording",
					Rules: []monitoringv1.Rule{
						{
							Record: "operator:reconcile_rate:5m",
							Expr:   intstr.FromString(fmt.Sprintf(`sum by (controller, job, namespace) (rate(controller_runtime_reconcile_total{%s}[5m]))`, scope)),
						},
						{
							// Churn: reconciles that are NOT the interval-driven
							// "come back in sync-interval" shape. The interval
							// controllers (Topic, User, Schema, ...) end every
							// healthy pass with RequeueAfter, which
							// controller-runtime counts as result="requeue_after",
							// so the total reconcile rate scales with CR count ×
							// engaged clusters / sync interval on a perfectly
							// healthy install. Excluding only that result keeps
							// errors, explicit requeues, AND success-result spin
							// loops (a controller re-triggering itself by
							// rewriting status converges to result="success" at
							// high rate) visible. See K8S-927.
							Record: "operator:reconcile_churn_rate:5m",
							Expr:   intstr.FromString(fmt.Sprintf(`sum by (controller, job, namespace) (rate(controller_runtime_reconcile_total{result!="requeue_after", %s}[5m]))`, scope)),
						},
						{
							Record: "operator:reconcile_error_rate:5m",
							Expr:   intstr.FromString(fmt.Sprintf(`sum by (controller, job, namespace) (rate(controller_runtime_reconcile_errors_total{%s}[5m]))`, scope)),
						},
						{
							Record: "operator:reconcile_steady_state_rate:5m",
							Expr:   intstr.FromString(fmt.Sprintf(`sum by (controller, job, namespace) (rate(operator_controller_reconcile_steady_state_total{%s}[5m]))`, scope)),
						},
						{
							Record: "operator:reconcile_p99_seconds:5m",
							Expr: intstr.FromString(fmt.Sprintf(
								`histogram_quantile(0.99, sum by (le, controller, job, namespace) (rate(controller_runtime_reconcile_time_seconds_bucket{%s}[5m])))`,
								scope,
							)),
						},
						{
							// Time spent waiting in the workqueue before a worker
							// picks the item up. This, not the busy-worker gauge,
							// is the backlog signal: workqueue metrics label the
							// queue with `name` (== the controller name).
							Record: "operator:workqueue_p99_seconds:5m",
							Expr: intstr.FromString(fmt.Sprintf(
								`histogram_quantile(0.99, sum by (le, name, job, namespace) (rate(workqueue_queue_duration_seconds_bucket{%s}[5m])))`,
								scope,
							)),
						},
					},
				},
				{
					Name: "redpanda-operator.controller-runtime.alerts",
					Rules: []monitoringv1.Rule{
						{
							// Sustained error rate on any controller. Healthy
							// controllers see transient errors; a sustained
							// 0.1/s rate (~6/min) over five minutes means
							// something is wedged.
							Alert: "OperatorReconcileErrors",
							Expr:  intstr.FromString(fmt.Sprintf(`operator:reconcile_error_rate:5m{%s} > 0.1`, scope)),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} in {{ $labels.namespace }} is failing reconciles",
								"description": "Controller {{ $labels.controller }} has been returning errors at >0.1/s for 5+ minutes. Check the operator pod logs and the relevant resource's status.",
							},
						},
						{
							// Runaway reconcile churn on a stable cluster.
							// Deliberately reads the churn rate, not the total
							// rate: the interval-driven controllers requeue
							// every CR every sync interval, so the total rate
							// is proportional to CR count and a fleet of a few
							// hundred Topics trips any fixed threshold while
							// perfectly healthy (K8S-927). Churn — anything but
							// result="requeue_after" — stays near zero on a
							// stable cluster.
							Alert: "OperatorReconcileRunaway",
							Expr:  intstr.FromString(fmt.Sprintf(`operator:reconcile_churn_rate:5m{%s} > 5`, scope)),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} in {{ $labels.namespace }} is reconciling at a high rate",
								"description": "Controller {{ $labels.controller }} has completed >5 reconciles per second for 5+ minutes, excluding scheduled interval requeues. On a stable cluster this means the controller is spinning on the same resources without reaching steady state. Cross-check operator_controller_reconcile_steady_state_total — if it is flat while this rate is high, the controller is making no progress.",
							},
						},
						{
							// Controller has stopped reconciling entirely.
							// Some controllers are normally idle, so this
							// alert only fires when a controller had
							// non-zero activity in the past and then went
							// silent — i.e. it should have been doing work
							// and stopped.
							Alert: "OperatorReconcileStalled",
							Expr: intstr.FromString(fmt.Sprintf(
								`max_over_time(operator:reconcile_rate:5m{%s}[1h]) > 0 and operator:reconcile_rate:5m{%s} == 0`,
								scope, scope,
							)),
							For: ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} in {{ $labels.namespace }} has stopped reconciling",
								"description": "Controller {{ $labels.controller }} was active in the last hour but has reconciled zero times in the past 10 minutes. The controller may have stopped responding to events.",
							},
						},
						{
							// Worker pool saturation, measured as time-in-queue
							// rather than the busy-worker gauge. The controllers
							// run one worker each, and on interval-driven
							// controllers with a few dozen CRs that single
							// worker legitimately sits near 100% busy while the
							// queue stays empty — the sampled
							// active>=max_concurrent comparison read that as
							// saturation on a healthy install (K8S-927). Items
							// waiting >30s at p99 for 10 minutes is an actual
							// backlog: work is arriving faster than the pool
							// drains it.
							Alert: "OperatorWorkerPoolSaturated",
							Expr:  intstr.FromString(fmt.Sprintf(`operator:workqueue_p99_seconds:5m{%s} > 30`, scope)),
							For:   ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.name }} in {{ $labels.namespace }} reconcile queue is backlogged",
								"description": "Items on controller {{ $labels.name }}'s workqueue have waited >30s at p99 for 10+ minutes: work arrives faster than the worker pool drains it. Consider increasing MaxConcurrentReconciles or investigating per-reconcile latency (operator:reconcile_p99_seconds:5m).",
							},
						},
					},
				},
				{
					Name: "redpanda-operator.stretchcluster.alerts",
					Rules: []monitoringv1.Rule{
						{
							// A peer cluster has been unreachable for 2 minutes.
							// Reachability is sampled by the multicluster
							// manager's background probe and surfaced through
							// the operator_stretchcluster_member_reachable
							// gauge. Healthy clusters self-recover quickly.
							Alert: "StretchClusterMemberUnreachable",
							Expr:  intstr.FromString(fmt.Sprintf(`operator_stretchcluster_member_reachable{%s} == 0`, scope)),
							For:   ptrDuration("2m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator cannot reach StretchCluster member {{ $labels.member }}",
								"description": "The multicluster manager's reachability probe has reported member {{ $labels.member }} of StretchCluster {{ $labels.stretchcluster }} as unreachable for 2+ minutes. Sustained unreachability is a real network outage or a peer apiserver that's down — sync operations targeting that peer (CA, bootstrap user, status updates) will be skipped until it recovers.",
							},
						},
						{
							// Desired brokers exceed ready brokers on a member —
							// that member is mid-rollout or has partially-failed
							// pods. Brief drift is expected during scale-up or
							// rolling restarts; sustained drift means a pod
							// won't come up.
							Alert: "StretchClusterBrokerCountSkew",
							Expr:  intstr.FromString(fmt.Sprintf(`operator_stretchcluster_brokers{%s} - operator_stretchcluster_brokers_ready{%s} > 0`, scope, scope)),
							For:   ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "StretchCluster {{ $labels.stretchcluster }} member {{ $labels.member }} has fewer ready brokers than desired",
								"description": "Member {{ $labels.member }} of StretchCluster {{ $labels.stretchcluster }} has had ready_brokers < desired_brokers for 10+ minutes. Expected briefly during scale operations; sustained skew means a broker pod is failing to start or pass readiness.",
							},
						},
						{
							// A member's local StretchCluster.spec differs from
							// the leader's. Almost always a stale manifest
							// (someone kubectl-applied an outdated CR to one
							// peer) — reconciliation is blocked on every peer
							// until the operator sees identical specs.
							Alert: "StretchClusterSpecDrift",
							Expr:  intstr.FromString(fmt.Sprintf(`operator_stretchcluster_spec_drift{%s} > 0`, scope)),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "StretchCluster {{ $labels.stretchcluster }} spec has drifted on member {{ $labels.member }}",
								"description": "Member {{ $labels.member }}'s local StretchCluster.spec differs from this operator's view for 5+ minutes. Reconciliation is blocked across all peers until specs match — reapply the canonical manifest to {{ $labels.member }} to clear the drift.",
							},
						},
						{
							// Replication-health gauge from the admin API. A
							// stretch cluster that stays unhealthy for 5
							// minutes is in a state that needs a human (e.g. a
							// majority of peers unreachable, partitions
							// under-replicated).
							Alert: "StretchClusterReplicationUnhealthy",
							Expr:  intstr.FromString(fmt.Sprintf(`operator_stretchcluster_replication_health{%s} == 0`, scope)),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "StretchCluster {{ $labels.stretchcluster }} reports unhealthy replication",
								"description": "The admin API reports StretchCluster {{ $labels.stretchcluster }} as unhealthy for 5+ minutes. Check broker readiness across members, cross-region network health, and partition replication status (rpk cluster health).",
							},
						},
					},
				},
			},
		},
	}
}

// ptrDuration is a small helper to convert a string into a
// *monitoringv1.Duration. Kept local to this file because gotohelm
// dislikes shared generic helpers across compilation units.
func ptrDuration(d string) *monitoringv1.Duration {
	dur := monitoringv1.Duration(d)
	return &dur
}
