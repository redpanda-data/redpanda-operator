#!/usr/bin/env bash
# Reproduces findings #2 (AdminClientForStretch drops ClientTimeout) and
# #3 (reconcile has no WithTimeout).
#
# One injection exercises both: apply tc-netem delay on the leader operator's
# egress to every broker's admin HTTP port (default 9644). Then look at the
# leader's StretchCluster reconcile "elapsed" field.
#
# Expected outcomes (unfixed code):
#   - Baseline reconcile elapsed: well under 1s.
#   - Chaotic reconcile elapsed: >= 10s. Commonly ~30s because rpadmin's
#     internal default http.Client.Timeout of 10s fires on each retry and
#     the client retries ~3× across the broker seed list.
#   - "Client.Timeout exceeded while awaiting headers" appears in operator
#     error logs — proves the operator-configured --admin-client-timeout is
#     not in effect on the Stretch path.
#
# Expected outcomes (if fixes are applied):
#   - Reconcile fails with `context.DeadlineExceeded` once the reconcile-level
#     WithTimeout fires (<= 2m).
#   - rpadmin's own timeout is overridden by the operator's configured value
#     (e.g. 10s, or whatever --admin-client-timeout is).

set -euo pipefail
MC_SCRIPT_TAG="finding-02-03"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_lib.sh
source "${HERE}/_lib.sh"

WINDOW_SECONDS=100
LATENCY_MS=30000
ADMIN_PORT="${MC_ADMIN_PORT:-9644}"
JOB_NAME="pumba-finding-02-03"
DEBUG_POD="tc-debug-finding-02-03"

remaining="$(mc::parse_common_flags "$@" || true)"
set --
if [[ -n "${remaining}" ]]; then
  while IFS= read -r a; do set -- "$@" "${a}"; done <<<"${remaining}"
fi

while (( $# > 0 )); do
  case "$1" in
    --window-seconds=*) WINDOW_SECONDS="${1#*=}" ;;
    --latency-ms=*)     LATENCY_MS="${1#*=}" ;;
    --admin-port=*)     ADMIN_PORT="${1#*=}" ;;
    -h|--help)
      cat <<EOF
Reproduces findings #2 and #3 — admin-client timeout on the Stretch path and
missing reconcile-level deadline. A single latency injection surfaces both.

Usage: $(basename "$0") [--window-seconds=<N>] [--latency-ms=<M>] [--admin-port=<P>] [common flags]

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
log "targeting tcp/${ADMIN_PORT} outbound from the leader operator"

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

regex="re2:.*${MC_CLUSTER_PODS[leader_idx]}.*"
log "launching Pumba netem delay ${LATENCY_MS}ms on leader egress (port ${ADMIN_PORT})"
mc::pumba_stop "${leader_idx}" "${JOB_NAME}"
mc::pumba_launch "${leader_idx}" "${JOB_NAME}" \
  netem \
  --duration "$(( WINDOW_SECONDS + 30 ))s" \
  --tc-image "${MC_PUMBA_TC_IMAGE}" \
  --ingress-port "${ADMIN_PORT}" \
  delay --time "${LATENCY_MS}" "${regex}"
mc::pumba_wait_ready "${leader_idx}" "${JOB_NAME}"
log "Pumba Ready. Observing leader for ${WINDOW_SECONDS}s"
sleep "${WINDOW_SECONDS}"

set +o pipefail
elapsed_raw="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since="${WINDOW_SECONDS}s" 2>/dev/null \
  | grep 'MulticlusterReconciler.Reconcile' | grep 'Finished reconciling' \
  | grep -oE '"elapsed":[0-9.]+' | cut -d: -f2 | sort -nr | head -1)"
set -o pipefail
elapsed_raw="${elapsed_raw:-0}"

set +o pipefail
admin_err_count="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since="${WINDOW_SECONDS}s" 2>/dev/null \
  | grep -c 'Client\.Timeout exceeded while awaiting headers')"
set -o pipefail
admin_err_count="${admin_err_count:-0}"

echo
echo "=================== Findings #2 + #3 repro ==================="
echo "Window:                    ${WINDOW_SECONDS}s"
echo "Injected delay:            ${LATENCY_MS}ms on leader→admin (tcp/${ADMIN_PORT})"
echo "Leader:                    ${MC_CLUSTER_LABELS[leader_idx]} / ${MC_CLUSTER_PODS[leader_idx]}"
echo "Longest reconcile elapsed: ${elapsed_raw}s"
echo "Client.Timeout errors:     ${admin_err_count}"
echo

elapsed_int="${elapsed_raw%%.*}"
elapsed_int="${elapsed_int:-0}"
if (( elapsed_int >= 10 )); then
  echo "RESULT: Finding #3 CONFIRMED — reconcile wall time (${elapsed_raw}s) exceeds 10s"
  echo "        with no reconcile-level deadline firing."
  if (( admin_err_count > 0 )); then
    echo "        Finding #2 CONFIRMED — 'Client.Timeout exceeded' confirms rpadmin's"
    echo "        internal 10s default is the only ceiling; --admin-client-timeout is"
    echo "        bypassed on the Stretch path."
  fi
  echo "==============================================================="
  exit 0
else
  echo "RESULT: NOT REPRODUCED — longest reconcile was ${elapsed_raw}s (<10s)."
  echo "Try a longer --latency-ms or ensure --admin-port matches the cluster."
  echo "==============================================================="
  exit 1
fi
