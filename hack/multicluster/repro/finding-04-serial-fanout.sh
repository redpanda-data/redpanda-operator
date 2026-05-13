#!/usr/bin/env bash
# Reproduces finding #4 (serial fanout across clusters in several reconcile
# phases — status writes, license lookup, Phase 1 scans — with no
# reachability check).
#
# Scenario: apply moderate latency (3s default) on ONE peer's K8s API
# ClusterIP only. If the reconcile iterates clusters serially, adding the
# delay on just one peer blows up total reconcile wall time by a multiple
# of the injected delay. A parallel implementation would cap at roughly
# max(per-cluster-latency).
#
# Expected outcomes (unfixed code):
#   injected reconcile wall time >> baseline; usually >= 2× the per-hop
#   injected delay, because the slow peer is hit from multiple serial sites.
#
# Expected outcomes (if fanout was parallelised with errgroup):
#   injected reconcile wall time ≈ baseline + injected latency.

set -euo pipefail
MC_SCRIPT_TAG="finding-04"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_lib.sh
source "${HERE}/_lib.sh"

WINDOW_SECONDS=180
LATENCY_MS=3000
JOB_NAME="pumba-finding-04"
DEBUG_POD="tc-debug-finding-04"
# Tail pause after the main window — serial fanout can produce single
# reconciles that take 90-120s. Without a tail pause our `kubectl logs`
# query fires before the long reconcile has written its Finished line.
TAIL_PAUSE_SECONDS="${TAIL_PAUSE_SECONDS:-180}"

remaining="$(mc::parse_common_flags "$@" || true)"
set --
if [[ -n "${remaining}" ]]; then
  while IFS= read -r a; do set -- "$@" "${a}"; done <<<"${remaining}"
fi

while (( $# > 0 )); do
  case "$1" in
    --window-seconds=*)    WINDOW_SECONDS="${1#*=}" ;;
    --latency-ms=*)        LATENCY_MS="${1#*=}" ;;
    --tail-pause-seconds=*) TAIL_PAUSE_SECONDS="${1#*=}" ;;
    -h|--help)
      cat <<EOF
Reproduces finding #4 — serial fanout across clusters. Injects latency on
one peer's K8s API and measures how much reconcile wall time blows up.

Usage: $(basename "$0") [--window-seconds=<N>] [--latency-ms=<M>] [--tail-pause-seconds=<T>] [common flags]

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

# Pick a peer — first non-leader entry.
peer_idx=-1
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  if (( i != leader_idx )); then peer_idx="${i}"; break; fi
done
(( peer_idx >= 0 )) || die "failed to pick a peer"
peer_api_ip="${MC_CLUSTER_API_IPS[peer_idx]}"
[[ -n "${peer_api_ip}" ]] || die "peer API IP empty for ${MC_CLUSTER_LABELS[peer_idx]}"
log "impairing leader→${MC_CLUSTER_LABELS[peer_idx]} K8s API: ${peer_api_ip}:${MC_API_PORT} (+${LATENCY_MS}ms)"

# Baseline: median elapsed from recent reconciles. Median is more robust
# against a stale outlier that may linger in logs from prior chaos.
set +o pipefail
baseline_samples="$(mc::kubectl "${leader_idx}" logs -n "${MC_CLUSTER_NSS[leader_idx]}" "${MC_CLUSTER_PODS[leader_idx]}" --since=2m 2>/dev/null \
  | grep 'MulticlusterReconciler.Reconcile' | grep 'Finished reconciling' \
  | grep -oE '"elapsed":[0-9.]+' | cut -d: -f2)"
set -o pipefail
if [[ -z "${baseline_samples}" ]]; then
  baseline_max=0
else
  baseline_max="$(printf '%s\n' "${baseline_samples}" | sort -n | awk '
    { a[NR]=$1 }
    END { if (NR==0) { print 0; exit } print a[int((NR+1)/2)] }
  ')"
fi
log "pre-injection median reconcile elapsed (last 2m): ${baseline_max}s"

mc::debug_pod_ensure "${leader_idx}" "${DEBUG_POD}"

cleanup() {
  local rc=$?
  log "cleaning up"
  # Clean on every cluster in case leadership moved during chaos and a
  # different cluster wound up with a tc qdisc from a transient nsenter.
  for i in "${!MC_CLUSTER_LABELS[@]}"; do
    mc::pumba_stop "${i}" "${JOB_NAME}" || true
  done
  mc::debug_pod_clear_tc "${leader_idx}" "${DEBUG_POD}"
  exit "${rc}"
}
trap cleanup EXIT INT TERM

mc::debug_pod_clear_tc "${leader_idx}" "${DEBUG_POD}"

regex="re2:.*${MC_CLUSTER_PODS[leader_idx]}.*"
log "launching Pumba netem delay ${LATENCY_MS}ms on leader egress to ${MC_CLUSTER_LABELS[peer_idx]} API"
mc::pumba_stop "${leader_idx}" "${JOB_NAME}"
# Pumba duration must cover window + tail-pause so chaos stays active while
# long reconciles in flight finish and write their elapsed.
mc::pumba_launch "${leader_idx}" "${JOB_NAME}" \
  netem \
  --duration "$(( WINDOW_SECONDS + TAIL_PAUSE_SECONDS + 30 ))s" \
  --tc-image "${MC_PUMBA_TC_IMAGE}" \
  --target "${peer_api_ip}/32" \
  --ingress-port "${MC_API_PORT}" \
  delay --time "${LATENCY_MS}" "${regex}"
mc::pumba_wait_ready "${leader_idx}" "${JOB_NAME}"

# Pumba sidecar takes a few seconds to attach tc. Pad the start timestamp
# to avoid counting pre-chaos reconciles.
sleep 10
start_ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "tc active since ~${start_ts}. Observing leader for ${WINDOW_SECONDS}s, then tail-pause ${TAIL_PAUSE_SECONDS}s"
sleep "${WINDOW_SECONDS}"

# Tail pause: give any in-flight reconcile that started during the main
# window time to actually finish and flush its Finished-reconciling log.
# Otherwise --since-time=start_ts returns a StretchCluster reconcile with
# Starting but no matching Finished line.
log "waiting an extra ${TAIL_PAUSE_SECONDS}s for long reconciles to finish"
sleep "${TAIL_PAUSE_SECONDS}"

# Scan ALL discovered clusters. A moderate delay on one peer is enough to
# occasionally trip CheckQuorum (finding #1) and move leadership — in that
# case the long reconcile recorded by the NEW leader is what we want.
injected_max=0
winning_label=""
for i in "${!MC_CLUSTER_LABELS[@]}"; do
  mc::refresh_idx "${i}" || continue
  set +o pipefail
  m="$(mc::kubectl "${i}" logs -n "${MC_CLUSTER_NSS[i]}" "${MC_CLUSTER_PODS[i]}" --since-time="${start_ts}" 2>/dev/null \
    | grep 'MulticlusterReconciler.Reconcile' | grep 'Finished reconciling' \
    | grep -oE '"elapsed":[0-9.]+' | cut -d: -f2 | sort -nr | head -1)"
  set -o pipefail
  m="${m:-0}"
  is_bigger=$(awk -v a="${m}" -v b="${injected_max}" 'BEGIN { print (a+0 > b+0) ? 1 : 0 }')
  if (( is_bigger == 1 )); then
    injected_max="${m}"
    winning_label="${MC_CLUSTER_LABELS[i]}"
  fi
done

latency_s=$(awk -v l="${LATENCY_MS}" 'BEGIN { printf "%.3f", l/1000 }')
ratio=$(awk -v a="${injected_max}" -v b="${baseline_max}" 'BEGIN {
  if (b+0==0) { print "inf"; exit }
  printf "%.1f", a/b
}')
multiple_of_delay=$(awk -v a="${injected_max}" -v l="${LATENCY_MS}" 'BEGIN {
  printf "%.1f", a/(l/1000)
}')

echo
echo "=================== Finding #4 repro ==================="
echo "Window / tail-pause:           ${WINDOW_SECONDS}s + ${TAIL_PAUSE_SECONDS}s"
echo "Injected:                      ${LATENCY_MS}ms delay on leader→${MC_CLUSTER_LABELS[peer_idx]} API (${peer_api_ip}:${MC_API_PORT})"
echo "Baseline median reconcile:     ${baseline_max}s"
echo "Injected max reconcile:        ${injected_max}s (from ${winning_label:-n/a})"
echo "Ratio (injected/baseline):     ${ratio}×"
echo "Injected / per-hop delay:      ${multiple_of_delay}×  (serial fanout ⇒ >1.5)"
echo

very_slow=$(awk -v l="${LATENCY_MS}" 'BEGIN { printf "%.3f", (l/1000)*2.0 }')
slow_thresh=$(awk -v l="${LATENCY_MS}" 'BEGIN { printf "%.3f", (l/1000)*1.5 }')

awk -v a="${injected_max}" -v vs="${very_slow}" -v st="${slow_thresh}" -v l="${latency_s}" '
BEGIN {
  if (a+0 >= vs+0) {
    print "RESULT: CONFIRMED — reconcile wall time is >=2× the per-hop delay, which"
    print "        means multiple serial sites (syncStatus, setupLicense, Phase 1"
    print "        scans) blocked on the single slow peer. Parallel fanout would"
    print "        have capped the reconcile near " l "s."
    exit 0
  } else if (a+0 >= st+0) {
    print "RESULT: LIKELY CONFIRMED — reconcile took more than the pure per-hop"
    print "        delay but less than 2×. Either one serial site + background"
    print "        cost, or parallel with some sequential waits. Run a longer"
    print "        window or bump --latency-ms to tighten the signal."
    exit 0
  } else {
    print "RESULT: NOT REPRODUCED — reconcile stayed near the per-hop delay (" l "s)."
    print "        Could mean the serial sites are short-circuiting, or the filter"
    print "        missed. Verify the peer API IP traffic using tc counters."
    exit 1
  }
}'
status=$?
echo "========================================================"
exit "${status}"
