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
func PrometheusRule(dot *helmette.Dot) *monitoringv1.PrometheusRule {
	values := helmette.Unwrap[Values](dot.Values)

	if !values.Monitoring.RulesEnabled {
		return nil
	}

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
							Expr:   intstr.FromString(`sum by (controller) (rate(controller_runtime_reconcile_total[5m]))`),
						},
						{
							Record: "operator:reconcile_error_rate:5m",
							Expr:   intstr.FromString(`sum by (controller) (rate(controller_runtime_reconcile_errors_total[5m]))`),
						},
						{
							Record: "operator:reconcile_steady_state_rate:5m",
							Expr:   intstr.FromString(`sum by (controller) (rate(operator_controller_reconcile_steady_state_total[5m]))`),
						},
						{
							Record: "operator:reconcile_p99_seconds:5m",
							Expr: intstr.FromString(
								`histogram_quantile(0.99, sum by (le, controller) (rate(controller_runtime_reconcile_time_seconds_bucket[5m])))`,
							),
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
							Expr:  intstr.FromString(`operator:reconcile_error_rate:5m > 0.1`),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} is failing reconciles",
								"description": "Controller {{ $labels.controller }} has been returning errors at >0.1/s for 5+ minutes. Check the operator pod logs and the relevant resource's status.",
							},
						},
						{
							// Runaway reconcile rate on a stable cluster.
							// Healthy controllers reach steady state and
							// rarely fire — a sustained >5/s is almost
							// always a controller spinning on the same
							// resource without making progress.
							Alert: "OperatorReconcileRunaway",
							Expr:  intstr.FromString(`operator:reconcile_rate:5m > 5`),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} is reconciling at a high rate",
								"description": "Controller {{ $labels.controller }} has been reconciling at >5/s for 5+ minutes. On a stable cluster this is usually a self-triggered reconcile loop. Cross-check operator_controller_reconcile_steady_state_total — if it is flat while the reconcile rate is high, the controller is spinning.",
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
							Expr: intstr.FromString(
								`max_over_time(operator:reconcile_rate:5m[1h]) > 0 and operator:reconcile_rate:5m == 0`,
							),
							For: ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} has stopped reconciling",
								"description": "Controller {{ $labels.controller }} was active in the last hour but has reconciled zero times in the past 10 minutes. The controller may have stopped responding to events.",
							},
						},
						{
							// Worker pool saturation. Workers pegged at
							// max_concurrent_reconciles for >10m means the
							// queue is consistently full — either the
							// controller is too slow or
							// MaxConcurrentReconciles is misconfigured.
							Alert: "OperatorWorkerPoolSaturated",
							Expr: intstr.FromString(
								`controller_runtime_active_workers >= controller_runtime_max_concurrent_reconciles`,
							),
							For: ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} worker pool saturated",
								"description": "Controller {{ $labels.controller }} has all reconcile workers busy for 10+ minutes. Reconciles may be queueing. Consider increasing MaxConcurrentReconciles or investigating per-reconcile latency.",
							},
						},
						{
							// Generation drift. The controller has seen a
							// newer spec than its last successful
							// reconciliation reflected in status. Brief
							// drift is normal during reconciles; sustained
							// drift > 60s usually means the controller is
							// failing to make progress on that resource.
							//
							// Fires only when controllers opt into
							// observability.RecordObservedGeneration —
							// the metric is silent on controllers that
							// don't record it.
							Alert: "OperatorObservedGenerationDrift",
							Expr:  intstr.FromString(`operator_controller_reconcile_observed_generation_drift > 0`),
							For:   ptrDuration("5m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} is behind on resource generation",
								"description": "Controller {{ $labels.controller }} on kind {{ $labels.kind }} has had observedGeneration < generation for 5+ minutes. The controller has seen newer specs than its last successful reconciliation.",
							},
						},
						{
							// Non-determinism detector. Increments mean the
							// controller wrote a spec whose hash differed
							// from the previous render but the API server
							// found no meaningful change. Almost always a
							// timestamp, map iteration order, or similar
							// instability in the operator's spec-rendering
							// code.
							Alert: "OperatorNonDeterministicSpec",
							Expr: intstr.FromString(
								`rate(operator_controller_reconcile_spec_hash_changed_without_generation_total[10m]) > 0`,
							),
							For: ptrDuration("10m"),
							Labels: map[string]string{
								"severity": "warning",
							},
							Annotations: map[string]string{
								"summary":     "Redpanda operator controller {{ $labels.controller }} is rendering non-deterministic specs",
								"description": "Controller {{ $labels.controller }} on kind {{ $labels.kind }} has been writing spec updates that the API server treats as no-ops (generation does not advance) but whose rendered hash differs run-to-run. This is a strong signal of non-determinism in the spec-rendering code.",
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
							Expr:  intstr.FromString(`operator_stretchcluster_member_reachable == 0`),
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
							Expr:  intstr.FromString(`operator_stretchcluster_brokers - operator_stretchcluster_brokers_ready > 0`),
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
							Expr:  intstr.FromString(`operator_stretchcluster_spec_drift > 0`),
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
							Expr:  intstr.FromString(`operator_stretchcluster_replication_health == 0`),
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
