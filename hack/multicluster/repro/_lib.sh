#!/usr/bin/env bash
# Shared helpers for the finding-XX repro scripts. Source this file from
# each script before parsing flags. Supports two discovery modes:
#
#   - vcluster-on-k3d (default):
#         assumes `task dev:setup-multicluster-dev-env` has produced three
#         vclusters under a single host k3d cluster, with host namespaces
#         named `vc-<prefix>-{0,1,2}`.
#
#   - multi-context (real clusters):
#         pass `--contexts=ctxA,ctxB,ctxC` on the script. Each context is
#         used directly (EKS/GKE/AKS/kind/etc). The operator is assumed to
#         live in namespace `$MC_NAMESPACE` (default `redpanda`) with the
#         usual helm-chart label selector.
#
# All environment variables below are overridable; the defaults fit the
# vcluster-on-k3d setup so the scripts work with no arguments.

MC_NAMESPACE="${MC_NAMESPACE:-redpanda}"
MC_OPERATOR_LABEL="${MC_OPERATOR_LABEL:-app.kubernetes.io/name=operator,app.kubernetes.io/instance=redpanda}"
MC_OPERATOR_SERVICE="${MC_OPERATOR_SERVICE:-multicluster-operator}"
MC_CONTAINERD_SOCK="${MC_CONTAINERD_SOCK:-/run/k3s/containerd/containerd.sock}"
MC_PUMBA_IMAGE="${MC_PUMBA_IMAGE:-ghcr.io/alexei-led/pumba:1.0.6}"
MC_PUMBA_TC_IMAGE="${MC_PUMBA_TC_IMAGE:-ghcr.io/alexei-led/pumba-alpine-nettools:latest}"
MC_RAFT_PORT="${MC_RAFT_PORT:-9443}"
MC_API_PORT="${MC_API_PORT:-443}"

# Parallel arrays, populated by mc::discover. Index them together.
MC_CLUSTER_LABELS=()   # human-readable label (context or host ns)
MC_CLUSTER_CTXS=()     # kubectl context ("" = use current)
MC_CLUSTER_NSS=()      # namespace where the operator pod lives (as visible to `kubectl`)
MC_CLUSTER_PODS=()
MC_CLUSTER_NODES=()
MC_CLUSTER_RAFT_IPS=() # peer raft gRPC ClusterIP as other peers reach it
MC_CLUSTER_API_IPS=()  # peer K8s API endpoint as other peers reach it

log() { printf '[%s] %s\n' "${MC_SCRIPT_TAG:-mc}" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# Run kubectl bound to the idx-th cluster's context (empty = current ctx).
mc::kubectl() {
  local idx="$1"; shift
  local ctx="${MC_CLUSTER_CTXS[idx]:-}"
  if [[ -n "${ctx}" ]]; then
    kubectl --context="${ctx}" "$@"
  else
    kubectl "$@"
  fi
}

# Populate the parallel arrays. Either $MC_CONTEXTS is set (CSV of contexts)
# or we fall back to vcluster-on-k3d discovery via $MC_VC_PREFIX.
mc::discover() {
  if [[ -n "${MC_CONTEXTS:-}" ]]; then
    mc::discover_contexts
  else
    mc::discover_vcluster
  fi
  (( ${#MC_CLUSTER_LABELS[@]} >= 2 )) || die "discovered fewer than 2 clusters — nothing to repro"
}

mc::discover_contexts() {
  local ctxs=()
  IFS=',' read -r -a ctxs <<<"${MC_CONTEXTS}"
  (( ${#ctxs[@]} >= 2 )) || die "--contexts must list at least 2 contexts"
  local ctx
  for ctx in "${ctxs[@]}"; do
    local pod
    pod="$(kubectl --context="${ctx}" get pod -n "${MC_NAMESPACE}" -l "${MC_OPERATOR_LABEL}" \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    [[ -n "${pod}" ]] || { log "WARN: no operator pod in ${ctx}/${MC_NAMESPACE} — skipping"; continue; }
    local node
    node="$(kubectl --context="${ctx}" get pod -n "${MC_NAMESPACE}" "${pod}" -o jsonpath='{.spec.nodeName}')"
    local raft_ip
    raft_ip="$(kubectl --context="${ctx}" get svc -n "${MC_NAMESPACE}" "${MC_OPERATOR_SERVICE}" \
      -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
    [[ -n "${raft_ip}" ]] || die "no Service ${MC_NAMESPACE}/${MC_OPERATOR_SERVICE} in ctx=${ctx}"
    # For peer K8s API, resolve the server URL from the context's kubeconfig.
    # This is what the leader operator will dial when it reaches out via the
    # peer kubeconfig cache.
    local api_url api_host api_ip
    api_url="$(kubectl --context="${ctx}" config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true)"
    api_host="$(printf '%s' "${api_url}" | sed -E 's|^https?://([^:/]+).*|\1|')"
    if [[ "${api_host}" =~ ^[0-9.]+$ ]]; then
      api_ip="${api_host}"
    else
      api_ip="$(getent ahostsv4 "${api_host}" 2>/dev/null | awk '/STREAM/ { print $1; exit }' || true)"
      [[ -n "${api_ip}" ]] || api_ip="${api_host}"
    fi
    MC_CLUSTER_LABELS+=("${ctx}")
    MC_CLUSTER_CTXS+=("${ctx}")
    MC_CLUSTER_NSS+=("${MC_NAMESPACE}")
    MC_CLUSTER_PODS+=("${pod}")
    MC_CLUSTER_NODES+=("${node}")
    MC_CLUSTER_RAFT_IPS+=("${raft_ip}")
    MC_CLUSTER_API_IPS+=("${api_ip}")
  done
}

mc::discover_vcluster() {
  if [[ -z "${MC_VC_PREFIX:-}" ]]; then
    MC_VC_PREFIX="$(kubectl get ns -o name 2>/dev/null \
      | sed -n 's|^namespace/\(vc-[a-z0-9]*\)-0$|\1|p' | head -1)"
    [[ -n "${MC_VC_PREFIX}" ]] || die "cannot auto-detect vcluster prefix — pass --vc-prefix= or --contexts="
  fi
  local suffix
  for suffix in 0 1 2; do
    local ns="${MC_VC_PREFIX}-${suffix}"
    local pod
    pod="$(kubectl get pod -n "${ns}" -l "${MC_OPERATOR_LABEL}" \
      -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
    [[ -n "${pod}" ]] || continue
    local node
    node="$(kubectl get pod -n "${ns}" "${pod}" -o jsonpath='{.spec.nodeName}')"
    local raft_ip api_ip
    raft_ip="$(kubectl get svc -n "${ns}" "${MC_OPERATOR_SERVICE}-x-default-x-${ns}" \
      -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
    api_ip="$(kubectl get svc -n "${ns}" "${ns}" -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
    MC_CLUSTER_LABELS+=("${ns}")
    MC_CLUSTER_CTXS+=("")
    MC_CLUSTER_NSS+=("${ns}")
    MC_CLUSTER_PODS+=("${pod}")
    MC_CLUSTER_NODES+=("${node}")
    MC_CLUSTER_RAFT_IPS+=("${raft_ip}")
    MC_CLUSTER_API_IPS+=("${api_ip}")
  done
}

# Return the index of the cluster currently driving MulticlusterReconciler.
# grep -c is used instead of grep -q because pipefail trips on SIGPIPE when
# grep -q exits early.
mc::find_leader_idx() {
  local i
  for i in "${!MC_CLUSTER_LABELS[@]}"; do
    local count
    set +o pipefail
    count="$(mc::kubectl "${i}" logs -n "${MC_CLUSTER_NSS[i]}" "${MC_CLUSTER_PODS[i]}" --tail=400 2>/dev/null \
      | grep -c 'MulticlusterReconciler\.Reconcile')"
    set -o pipefail
    if (( ${count:-0} > 0 )); then
      printf '%s\n' "${i}"
      return 0
    fi
  done
  return 1
}

# Refresh pod/node info for one index. Useful after leadership moves and we
# want to target whichever operator pod is currently leader.
mc::refresh_idx() {
  local i="$1"
  local pod
  pod="$(mc::kubectl "${i}" get pod -n "${MC_CLUSTER_NSS[i]}" -l "${MC_OPERATOR_LABEL}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "${pod}" ]] || return 1
  MC_CLUSTER_PODS[i]="${pod}"
  MC_CLUSTER_NODES[i]="$(mc::kubectl "${i}" get pod -n "${MC_CLUSTER_NSS[i]}" "${pod}" -o jsonpath='{.spec.nodeName}')"
}

# Ensure a privileged netshoot pod exists on the same cluster+node as the
# idx-th operator. Used to nsenter into the operator pod's netns for tc
# cleanup. Idempotent.
mc::debug_pod_ensure() {
  local idx="$1"
  local pod_name="$2"
  if ! mc::kubectl "${idx}" get pod -n default "${pod_name}" >/dev/null 2>&1; then
    mc::kubectl "${idx}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata: { name: ${pod_name}, namespace: default }
spec:
  nodeName: ${MC_CLUSTER_NODES[idx]}
  hostPID: true
  hostNetwork: true
  containers:
  - name: debug
    image: nicolaka/netshoot
    command: ["sleep","3600"]
    securityContext: { privileged: true }
EOF
  fi
  mc::kubectl "${idx}" wait --for=condition=Ready pod/"${pod_name}" -n default --timeout=90s >/dev/null
}

# Delete any residual tc qdisc on the operator pod's netns in the cluster
# that the debug pod belongs to.
mc::debug_pod_clear_tc() {
  local idx="$1"
  local pod_name="$2"
  mc::kubectl "${idx}" exec -n default "${pod_name}" -- sh -c '
    for p in $(pgrep -f "^/redpanda-operator"); do
      nsenter -t $p -n tc qdisc del dev eth0 root 2>/dev/null || true
    done
  ' >/dev/null 2>&1 || true
}

# Launch a Pumba Job pinned to the idx-th cluster's leader node, passing
# the remaining arguments verbatim as the pumba binary args. The Job is
# deployed in the `default` namespace of that cluster.
#
# Usage: mc::pumba_launch <idx> <job_name> <pumba_arg> [pumba_arg...]
#
# The --runtime/--containerd-socket/--containerd-namespace prefix is added
# automatically so callers only supply the chaos-specific args (netem /
# iptables / etc.).
mc::pumba_launch() {
  local idx="$1"; shift
  local job_name="$1"; shift

  local -a full_args
  full_args=(
    "--log-level" "info"
    "--runtime"   "containerd"
    "--containerd-socket"    "${MC_CONTAINERD_SOCK}"
    "--containerd-namespace" "k8s.io"
    "$@"
  )

  local args_yaml=""
  local a
  for a in "${full_args[@]}"; do
    args_yaml+="            - \"${a}\"
"
  done

  mc::kubectl "${idx}" apply -f - >/dev/null <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job_name}
  namespace: default
spec:
  backoffLimit: 0
  ttlSecondsAfterFinished: 300
  template:
    spec:
      restartPolicy: Never
      nodeName: ${MC_CLUSTER_NODES[idx]}
      hostPID: true
      containers:
        - name: pumba
          image: ${MC_PUMBA_IMAGE}
          imagePullPolicy: IfNotPresent
          securityContext:
            privileged: true
          args:
${args_yaml}
          volumeMounts:
            - name: run
              mountPath: /run
              mountPropagation: Bidirectional
            - name: var-lib
              mountPath: /var/lib
              mountPropagation: Bidirectional
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: run
          hostPath: { path: /run, type: Directory }
        - name: var-lib
          hostPath: { path: /var/lib, type: Directory }
        - name: tmp
          emptyDir: {}
EOF
}

mc::pumba_wait_ready() {
  local idx="$1" job_name="$2"
  local _
  for _ in 1 2 3 4 5; do
    if mc::kubectl "${idx}" get pod -n default -l job-name="${job_name}" 2>/dev/null | grep -q .; then break; fi
    sleep 2
  done
  # 5 min covers first-time image pull on a cold node (Pumba ~100MB).
  mc::kubectl "${idx}" wait --for=condition=Ready pod -n default -l job-name="${job_name}" --timeout=300s >/dev/null \
    || die "Pumba job ${job_name} never became Ready"
}

mc::pumba_stop() {
  local idx="$1" job_name="$2"
  mc::kubectl "${idx}" delete job -n default "${job_name}" --ignore-not-found=true --wait=false >/dev/null 2>&1 || true
}

# Parse flags shared by every repro script. Call BEFORE script-specific
# flags are parsed; pass the original argv. This sets MC_CONTEXTS and
# MC_VC_PREFIX and consumes matching args; remaining args are re-emitted on
# stdout for the caller to re-parse.
mc::parse_common_flags() {
  local out=()
  while (( $# > 0 )); do
    case "$1" in
      --contexts=*)         MC_CONTEXTS="${1#*=}" ;;
      --vc-prefix=*)        MC_VC_PREFIX="${1#*=}" ;;
      --namespace=*)        MC_NAMESPACE="${1#*=}" ;;
      --operator-label=*)   MC_OPERATOR_LABEL="${1#*=}" ;;
      --operator-service=*) MC_OPERATOR_SERVICE="${1#*=}" ;;
      --containerd-sock=*)  MC_CONTAINERD_SOCK="${1#*=}" ;;
      --pumba-image=*)      MC_PUMBA_IMAGE="${1#*=}" ;;
      --tc-image=*)         MC_PUMBA_TC_IMAGE="${1#*=}" ;;
      *) out+=("$1") ;;
    esac
    shift
  done
  if (( ${#out[@]} > 0 )); then
    printf '%s\n' "${out[@]}"
  fi
}

# Print the shared-flags help snippet. Each script calls this at the top of
# its own usage block.
mc::print_common_flags() {
  cat <<'EOF'
Common flags (any mode):
  --contexts=<csv>          CSV of kube contexts, one per cluster. Enables multi-context
                            (real multi-cloud) mode. If omitted, script auto-detects a
                            vcluster-on-k3d layout under ~/.kube/config's current context.
  --vc-prefix=<prefix>      Override vcluster prefix (e.g. "vc-xyz"). Default: auto-detect.
  --namespace=<ns>          Operator namespace in each cluster. Default: redpanda.
  --operator-label=<sel>    Operator label selector. Default: app.kubernetes.io/name=operator,
                            app.kubernetes.io/instance=redpanda.
  --operator-service=<name> Operator gRPC Service name. Default: multicluster-operator.
  --containerd-sock=<path>  Host path to containerd socket. Default: /run/k3s/containerd/
                            containerd.sock (k3s/k3d). Use /run/containerd/containerd.sock
                            on EKS/GKE/AKS standard node pools.
  --pumba-image=<img>       Pumba image. Default: ghcr.io/alexei-led/pumba:1.0.6.
  --tc-image=<img>          Pumba sidecar tc-image. Default: ghcr.io/alexei-led/pumba-alpine-nettools:latest.

Environment variable equivalents: MC_NAMESPACE, MC_OPERATOR_LABEL,
MC_OPERATOR_SERVICE, MC_CONTAINERD_SOCK, MC_PUMBA_IMAGE, MC_PUMBA_TC_IMAGE.
EOF
}
