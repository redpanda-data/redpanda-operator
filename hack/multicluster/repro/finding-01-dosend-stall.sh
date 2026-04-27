#!/usr/bin/env bash
# Reproduces finding #1 (DoSend stall + CheckQuorum-driven leader churn under
# asymmetric silent packet loss on the raft gRPC link).
#
# Scenario: silently drop leader→peer traffic on the raft gRPC port (default
# 9443), leaving the reverse direction intact. Because `DoSend` in the raft
# Ready loop is synchronous and has no per-RPC timeout, a blackholed TCP
# write blocks the entire raft loop, including heartbeats to the OTHER
# peer. With CheckQuorum=true, the leader steps down even though a majority
# is healthy.
#
# Expected outcomes (unfixed code):
#   - At least one raft election ("starting a new election") in the window
#   - Leader identity changes (term increments) while only one peer is impaired
#
# Expected outcomes (if fixes are applied):
#   - gRPC keepalive + per-RPC deadline lets the leader fail fast on the
#     impaired peer and continue heartbeating the healthy one
#   - Zero or one stepdown; leadership stays stable

set -euo pipefail
MC_SCRIPT_TAG="finding-01"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_lib.sh
source "${HERE}/_lib.sh"

WINDOW_SECONDS=180
JOB_NAME="pumba-finding-01"
DEBUG_POD="tc-debug-finding-01"

# Parse common flags first (consumes --contexts, --namespace, etc.) and
# re-run argv parsing on the leftovers for script-specific flags.
remaining="$(mc::parse_common_flags "$@" || true)"
set --
if [[ -n "${remaining}" ]]; then
  while IFS= read -r a; do set -- "$@" "${a}"; done <<<"${remaining}"
fi

while (( $# > 0 )); do
  case "$1" in
    --window-seconds=*) WINDOW_SECONDS="${1#*=}" ;;
    -h|--help)
      cat <<EOF
Reproduces finding #1 — asymmetric silent drop on the raft gRPC port causes
leader stepdown / election churn, even though a majority of peers are fine.

Usage: $(basename "$0") [--window-seconds=<N>] [common flags]

$(mc::print_common_flags)
EOF
      exit 0
      ;;
    *) die "unknown flag: $1 (run --help)" ;;
  esac
  shift
done

mc::discover

leader_idx="$(mc::find_leader_idx)" || die "no raft leader found across discovered clusters"
log "leader: ${MC_CLUSTER_LABELS[leader_idx]} pod=${MC_CLUSTER_PODS[leader_idx]} node=${MC_CLUSTER_NODES[leader_idx]}"

# Pick a peer — the first non-leader entry.
peer_idx=-1
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  if (( i != leader_idx )); then peer_idx="${i}"; break; fi
done
(( peer_idx >= 0 )) || die "failed to pick a peer"
peer_raft_ip="${MC_CLUSTER_RAFT_IPS[peer_idx]}"
[[ -n "${peer_raft_ip}" ]] || die "peer raft IP empty for ${MC_CLUSTER_LABELS[peer_idx]}"
log "impairing leader→${MC_CLUSTER_LABELS[peer_idx]} raft: ${peer_raft_ip}:${MC_RAFT_PORT}"

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

# Snapshot pre-injection election counts per cluster.
declare -a pre_elections
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  set +o pipefail
  c="$(mc::kubectl "${i}" logs -n "${MC_CLUSTER_NSS[i]}" "${MC_CLUSTER_PODS[i]}" --since=10m 2>/dev/null \
    | grep -c 'starting a new election')"
  set -o pipefail
  pre_elections[i]="${c:-0}"
done

regex="re2:.*${MC_CLUSTER_PODS[leader_idx]}.*"
log "launching Pumba netem loss 100% on leader → peer raft (asymmetric silent drop)"
mc::pumba_stop "${leader_idx}" "${JOB_NAME}"
mc::pumba_launch "${leader_idx}" "${JOB_NAME}" \
  netem \
  --duration "$(( WINDOW_SECONDS + 30 ))s" \
  --tc-image "${MC_PUMBA_TC_IMAGE}" \
  --target "${peer_raft_ip}/32" \
  --ingress-port "${MC_RAFT_PORT}" \
  loss --percent 100 "${regex}"
mc::pumba_wait_ready "${leader_idx}" "${JOB_NAME}"
log "Pumba Ready. Observing ${#MC_CLUSTER_LABELS[@]} operators for ${WINDOW_SECONDS}s"
sleep "${WINDOW_SECONDS}"

total_new_elections=0
summary=""
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  set +o pipefail
  cur="$(mc::kubectl "${i}" logs -n "${MC_CLUSTER_NSS[i]}" "${MC_CLUSTER_PODS[i]}" --since=10m 2>/dev/null \
    | grep -c 'starting a new election')"
  set -o pipefail
  cur="${cur:-0}"
  pre="${pre_elections[i]:-0}"
  delta=$(( cur - pre ))
  summary+=" ${MC_CLUSTER_LABELS[i]}:+${delta}"
  total_new_elections=$(( total_new_elections + delta ))
done

set +o pipefail
stepdowns="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since="${WINDOW_SECONDS}s" 2>/dev/null \
  | grep -cE 'became follower at term|stepped down')"
set -o pipefail
stepdowns="${stepdowns:-0}"

echo
echo "=================== Finding #1 repro ==================="
echo "Window:                       ${WINDOW_SECONDS}s"
echo "Injected:                     100% packet loss on leader→${MC_CLUSTER_LABELS[peer_idx]}:${MC_RAFT_PORT}"
echo "Leader (pre-injection):       ${MC_CLUSTER_LABELS[leader_idx]} / ${MC_CLUSTER_PODS[leader_idx]}"
echo "New raft elections by cluster:${summary}"
echo "Total new elections:          ${total_new_elections}"
echo "Leader 'became follower':     ${stepdowns}"
echo
if (( total_new_elections >= 1 || stepdowns >= 1 )); then
  echo "RESULT: CONFIRMED — asymmetric silent drop on a single peer link caused"
  echo "        leader election / stepdown on the CURRENT leader, even though a"
  echo "        majority of peers (2/3) remained reachable. This is the DoSend/"
  echo "        CheckQuorum behaviour described in finding #1."
  echo "========================================================"
  exit 0
else
  echo "RESULT: NOT REPRODUCED — leader stayed stable through the window."
  echo "Consider --window-seconds=300 or verify tc filter is applied on leader."
  echo "========================================================"
  exit 1
fi
