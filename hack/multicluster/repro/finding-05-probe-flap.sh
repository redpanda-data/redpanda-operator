#!/usr/bin/env bash
# Reproduces finding #5 (health-probe flap hazard at marginal RTT).
#
# Scenario: inject ~4.5s ± 500ms latency on the multicluster leader's egress
# to ONE peer's Kubernetes API server. The health probe at
# `pkg/multicluster/health.go` uses a 5s timeout and a 10s interval. With
# RTT sitting right at the threshold, some probes succeed and some don't,
# causing the "reachable / unreachable" status for that peer to flap.

set -euo pipefail
MC_SCRIPT_TAG="finding-05"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_lib.sh
source "${HERE}/_lib.sh"

WINDOW_SECONDS=120
LATENCY_MS=4500
JITTER_MS=500
JOB_NAME="pumba-finding-05"
DEBUG_POD="tc-debug-finding-05"

remaining="$(mc::parse_common_flags "$@" || true)"
set --
if [[ -n "${remaining}" ]]; then
  while IFS= read -r a; do set -- "$@" "${a}"; done <<<"${remaining}"
fi

while (( $# > 0 )); do
  case "$1" in
    --window-seconds=*) WINDOW_SECONDS="${1#*=}" ;;
    --latency-ms=*)     LATENCY_MS="${1#*=}" ;;
    --jitter-ms=*)      JITTER_MS="${1#*=}" ;;
    -h|--help)
      cat <<EOF
Reproduces finding #5 — health probe flaps between reachable/unreachable
when peer RTT sits near the 5s probe timeout.

Usage: $(basename "$0") [--window-seconds=<N>] [--latency-ms=<M>] [--jitter-ms=<J>] [common flags]

$(mc::print_common_flags)
EOF
      exit 0
      ;;
    *) die "unknown flag: $1 (run --help)" ;;
  esac
  shift
done

mc::discover
leader_idx="$(mc::find_leader_idx)" || die "no raft leader found"
log "leader: ${MC_CLUSTER_LABELS[leader_idx]} pod=${MC_CLUSTER_PODS[leader_idx]} node=${MC_CLUSTER_NODES[leader_idx]}"

peer_idx=-1
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  if (( i != leader_idx )); then peer_idx="${i}"; break; fi
done
(( peer_idx >= 0 )) || die "failed to pick a peer"
peer_api_ip="${MC_CLUSTER_API_IPS[peer_idx]}"
[[ -n "${peer_api_ip}" ]] || die "peer API IP empty for ${MC_CLUSTER_LABELS[peer_idx]}"
log "slowing leader→${MC_CLUSTER_LABELS[peer_idx]} K8s API: ${peer_api_ip}:${MC_API_PORT} (+${LATENCY_MS}±${JITTER_MS}ms)"

mc::debug_pod_ensure "${leader_idx}" "${DEBUG_POD}"

cleanup() {
  local rc=$?
  log "cleaning up"
  mc::pumba_stop "${leader_idx}" "${JOB_NAME}"
  mc::debug_pod_clear_tc "${leader_idx}" "${DEBUG_POD}"
  exit "${rc}"
}
trap cleanup EXIT INT TERM

mc::debug_pod_clear_tc "${leader_idx}" "${DEBUG_POD}"

set +o pipefail
pre_count="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since=5m 2>/dev/null \
  | grep -cE 'cluster became (reachable|unreachable)')"
set -o pipefail
pre_count="${pre_count:-0}"
log "pre-injection transition count (last 5m): ${pre_count}"

regex="re2:.*${MC_CLUSTER_PODS[leader_idx]}.*"
log "launching Pumba netem delay ${LATENCY_MS}ms ± ${JITTER_MS}ms on leader egress to peer API"
mc::pumba_stop "${leader_idx}" "${JOB_NAME}"
if (( JITTER_MS > 0 )); then
  mc::pumba_launch "${leader_idx}" "${JOB_NAME}" \
    netem \
    --duration "$(( WINDOW_SECONDS + 30 ))s" \
    --tc-image "${MC_PUMBA_TC_IMAGE}" \
    --target "${peer_api_ip}/32" \
    --ingress-port "${MC_API_PORT}" \
    delay --time "${LATENCY_MS}" --jitter "${JITTER_MS}" "${regex}"
else
  mc::pumba_launch "${leader_idx}" "${JOB_NAME}" \
    netem \
    --duration "$(( WINDOW_SECONDS + 30 ))s" \
    --tc-image "${MC_PUMBA_TC_IMAGE}" \
    --target "${peer_api_ip}/32" \
    --ingress-port "${MC_API_PORT}" \
    delay --time "${LATENCY_MS}" "${regex}"
fi
mc::pumba_wait_ready "${leader_idx}" "${JOB_NAME}"
log "Pumba Ready. Observing leader for ${WINDOW_SECONDS}s"
sleep "${WINDOW_SECONDS}"

set +o pipefail
post_count="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since="${WINDOW_SECONDS}s" 2>/dev/null \
  | grep -cE 'cluster became (reachable|unreachable)')"
set -o pipefail
post_count="${post_count:-0}"

echo
echo "=================== Finding #5 repro ==================="
echo "Window:                 ${WINDOW_SECONDS}s"
echo "Injected delay:         ${LATENCY_MS}ms ± ${JITTER_MS}ms"
echo "Leader:                 ${MC_CLUSTER_LABELS[leader_idx]} / ${MC_CLUSTER_PODS[leader_idx]}"
echo "Peer being slowed:      ${MC_CLUSTER_LABELS[peer_idx]} (API=${peer_api_ip})"
echo "Transitions in window:  ${post_count}"
echo
if (( post_count >= 2 )); then
  echo "RESULT: CONFIRMED — SpecSynced reachable/unreachable flapped ${post_count}x"
  echo "========================================================"
  exit 0
else
  echo "RESULT: NOT REPRODUCED — expected >=2 transitions, got ${post_count}"
  echo "Try a different --latency-ms around 4000-5000 and rerun."
  echo "========================================================"
  exit 1
fi
