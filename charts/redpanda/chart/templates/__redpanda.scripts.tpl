{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/scripts.go" */ -}}

{{- define "_redpanda.LifecycleCommonSh" -}}
{{- $curlURL := (index .a 0) -}}
{{- $adminCurlFlags := (index .a 1) -}}
{{- $podFQDN := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (join "\n" (list `#!/usr/bin/env bash` `` `# the SERVICE_NAME comes from the metadata.name of the pod, essentially the POD_NAME` (printf `CURL_URL="%s"` $curlURL) `` `# commands used throughout. Every curl carries --max-time so a single` `# hung admin connection can never freeze a lifecycle hook until the` `# termination grace period expires; the callers retry as needed.` (printf `CURL_NODE_ID_CMD="curl --silent --fail --max-time 5 %s ${CURL_URL}/v1/node_config"` $adminCurlFlags) `` `CURL_MAINTENANCE_DELETE_CMD_PREFIX='curl -X DELETE --silent --max-time 5 -o /dev/null -w "%{http_code}"'` `CURL_MAINTENANCE_PUT_CMD_PREFIX='curl -X PUT --silent --max-time 5 -o /dev/null -w "%{http_code}"'` (printf `CURL_MAINTENANCE_GET_CMD="curl -X GET --silent --max-time 5 %s ${CURL_URL}/v1/maintenance"` $adminCurlFlags) `` `# run bare to list all brokers, or append /<node id> for a single broker` (printf `CURL_BROKERS_CMD_PREFIX="curl --silent --fail --max-time 5 %s ${CURL_URL}/v1/brokers"` $adminCurlFlags) `` `# the address this pod's broker advertises to the rest of the cluster,` `# i.e. the internal_rpc_address its broker registers under` `# character-identical to the statefulset's --advertise-rpc-addr host` `POD_ORDINAL=${SERVICE_NAME##*-}` (printf `POD_FQDN="%s"` $podFQDN)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.LifecyclePostStartSh" -}}
{{- $adminCurlFlags := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (join "\n" (list `#!/usr/bin/env bash` `# This code should be similar if not exactly the same as that found in the panda-operator, see` `# https://github.com/redpanda-data/redpanda/blob/e51d5b7f2ef76d5160ca01b8c7a8cf07593d29b6/src/go/k8s/pkg/resources/secret.go` `` `# path below should match the path defined on the statefulset` `source /var/lifecycle/common.sh` `` `postStartHook () {` `  set -x` `` `  touch /tmp/postStartHookStarted` `` `  until NODE_ID=$(${CURL_NODE_ID_CMD} | grep -o '\"node_id\":[^,}]*' | grep -o '[^: ]*$'); do` `      sleep 0.5` `  done` `` `  echo "Clearing maintenance mode on node ${NODE_ID}"` (printf `  CURL_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"` $adminCurlFlags) `  # a 400 here would mean not in maintenance mode` `  until [ "${status:-}" = '"200"' ] || [ "${status:-}" = '"400"' ]; do` `      status=$(${CURL_MAINTENANCE_DELETE_CMD})` `      sleep 0.5` `  done` `` `  # A previous incarnation of this pod may have re-registered with the` `  # cluster under a different node id after losing its data directory` `  # (e.g. when running without persistent storage). preStop put that old` `  # node id into maintenance mode and nothing ever clears it: the ghost` `  # broker occupies the cluster's only maintenance-mode slot, and the` `  # partition autobalancer excludes maintenance-mode brokers from` `  # auto-decommission (redpanda-data/redpanda-operator#1674). Clear` `  # maintenance mode on any broker id that advertises this pod's address` `  # but isn't the current broker. Only a broker the cluster reports dead` `  # (is_alive=false) and draining is cleared — but the health monitor can` `  # lag a fast pod restart and briefly report the dead previous` `  # incarnation as still alive, so keep re-checking until every stale` `  # drainer at this address is gone; the surrounding timeout wrapper` `  # bounds this loop.` `  while true; do` `      GHOSTS_REMAIN=false` `      until BROKER_IDS=$(${CURL_BROKERS_CMD_PREFIX} | grep -o '\"node_id\": *[0-9-]*' | grep -o '[0-9-]*$'); do` `          sleep 0.5` `      done` `      for BROKER_ID in ${BROKER_IDS}; do` `          if [ "${BROKER_ID}" = "${NODE_ID}" ]; then continue; fi` `          BROKER=$(${CURL_BROKERS_CMD_PREFIX}/${BROKER_ID}) || continue` `          # compare the advertised address exactly (not as a regex, which` `          # would treat the dots in ${POD_FQDN} as wildcards)` `          BROKER_ADDRESS=$(echo "${BROKER}" | grep -o '\"internal_rpc_address\": *\"[^\"]*' | grep -o '[^\"]*$')` `          if [ "${BROKER_ADDRESS}" != "${POD_FQDN}" ]; then continue; fi` `          if ! echo "${BROKER}" | grep -q '\"draining\": *true'; then continue; fi` `          # a drainer at this pod's address still reported alive is the` `          # health monitor lagging behind the restart; retry until the` `          # cluster reports it dead` `          if ! echo "${BROKER}" | grep -q '\"is_alive\": *false'; then GHOSTS_REMAIN=true; continue; fi` `          echo "Clearing maintenance mode left behind by node ${BROKER_ID}, a previous incarnation of this pod"` (printf `          CURL_GHOST_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${BROKER_ID}/maintenance"` $adminCurlFlags) `          GHOST_STATUS=""` `          # a 400 means not in maintenance mode, a 404 means already removed` `          until [ "${GHOST_STATUS:-}" = '"200"' ] || [ "${GHOST_STATUS:-}" = '"400"' ] || [ "${GHOST_STATUS:-}" = '"404"' ]; do` `              GHOST_STATUS=$(${CURL_GHOST_MAINTENANCE_DELETE_CMD})` `              sleep 0.5` `          done` `      done` `      if [ "${GHOSTS_REMAIN}" = "false" ]; then break; fi` `      sleep 5` `  done` `` `  touch /tmp/postStartHookFinished` `}` `` `postStartHook` `true`))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.LifecyclePreStopSh" -}}
{{- $adminCurlFlags := (index .a 0) -}}
{{- $drain := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $preStopSh := (list `#!/usr/bin/env bash` `# This code should be similar if not exactly the same as that found in the panda-operator, see` `# https://github.com/redpanda-data/redpanda/blob/e51d5b7f2ef76d5160ca01b8c7a8cf07593d29b6/src/go/k8s/pkg/resources/secret.go` `` `touch /tmp/preStopHookStarted` `` `# path below should match the path defined on the statefulset` `source /var/lifecycle/common.sh` `` `set -x` `` `preStopHook () {` `  # Read this broker's node id. A broker being gracefully rolled answers` `  # immediately; a crashlooping / dead-admin broker never binds its admin` `  # API, so bound the attempts instead of spinning the whole termination` `  # grace period — if we cannot even read the node id there is nothing` `  # locally to drain, so skip the drain and let the pod terminate.` `  NODE_ID=""` `  for attempt in $(seq 1 20); do` `      NODE_ID=$(${CURL_NODE_ID_CMD} | grep -o '\"node_id\":[^,}]*' | grep -o '[^: ]*$') && break` `      NODE_ID=""` `      sleep 0.5` `  done` `  if [ -z "${NODE_ID}" ]; then` `      echo "Could not read node id from the local admin API; skipping maintenance drain."` `      touch /tmp/preStopHookFinished` `      return 0` `  fi` `` `  echo "Setting maintenance mode on node ${NODE_ID}"` (printf `  CURL_MAINTENANCE_PUT_CMD="${CURL_MAINTENANCE_PUT_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"` $adminCurlFlags) `  # A 404 means this broker's node id is no longer a cluster member -- e.g.` `  # a bad_rejoin broker whose id was decommissioned while its pod was down` `  # (the redpanda#31057 recovery path): there is nothing to drain, and the` `  # drain-status poll below would never terminate, so skip it instead of` `  # burning the hook timeout and delaying pod termination. A 400 (the` `  # cluster's single maintenance slot is busy with another draining broker)` `  # must keep retrying: waiting our turn for the slot is what serializes` `  # drains during rolling restarts.` `  until [ "${status:-}" = '"200"' ] || [ "${status:-}" = '"404"' ]; do` `      status=$(${CURL_MAINTENANCE_PUT_CMD})` `      sleep 0.5` `  done` `` `  if [ "${status:-}" = '"200"' ]; then` `      # Wait for the drain to finish. draining=false must NOT be accepted` `      # until we have first observed draining=true (or an explicit` `      # finished=true): immediately after the PUT the broker may not have` `      # started draining yet, and reading that initial draining=false as` `      # "done" would SIGTERM a broker still holding all its leadership.` `      saw_draining=""` `      finished=""` `      draining=""` `      until [ "${finished:-}" = "true" ] || { [ "${saw_draining:-}" = "yes" ] && [ "${draining:-}" = "false" ]; }; do` `          res=$(${CURL_MAINTENANCE_GET_CMD})` `          finished=$(echo $res | grep -o '\"finished\":[^,}]*' | grep -o '[^: ]*$')` `          draining=$(echo $res | grep -o '\"draining\":[^,}]*' | grep -o '[^: ]*$')` `          if [ "${draining:-}" = "true" ]; then saw_draining=yes; fi` `          sleep 0.5` `      done` `  fi` `` `  touch /tmp/preStopHookFinished` `}`) -}}
{{- if $drain -}}
{{- $preStopSh = (concat (default (list) $preStopSh) (list `preStopHook`)) -}}
{{- else -}}
{{- $preStopSh = (concat (default (list) $preStopSh) (list `touch /tmp/preStopHookFinished` `echo "Not enough replicas or in recovery mode, cannot put a broker into maintenance mode."`)) -}}
{{- end -}}
{{- $preStopSh = (concat (default (list) $preStopSh) (list `true`)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (join "\n" $preStopSh)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.FSValidatorSh" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" `set -e
EXPECTED_FS_TYPE=$1

DATA_DIR="/var/lib/redpanda/data"
TEST_FILE="testfile"

echo "checking data directory exist..."
if [ ! -d "${DATA_DIR}" ]; then
  echo "data directory does not exists, exiting"
  exit 1
fi

echo "checking filesystem type..."
FS_TYPE=$(df -T $DATA_DIR  | tail -n +2 | awk '{print $2}')

if [ "${FS_TYPE}" != "${EXPECTED_FS_TYPE}" ]; then
  echo "file system found to be ${FS_TYPE} when expected ${EXPECTED_FS_TYPE}"
  exit 1
fi

echo "checking if able to create a test file..."

touch ${DATA_DIR}/${TEST_FILE}
result=$(touch ${DATA_DIR}/${TEST_FILE} 2> /dev/null; echo $?)
if [ "${result}" != "0" ]; then
  echo "could not write testfile, may not have write permission"
  exit 1
fi

echo "checking if able to delete a test file..."

result=$(rm ${DATA_DIR}/${TEST_FILE} 2> /dev/null; echo $?)
if [ "${result}" != "0" ]; then
  echo "could not delete testfile"
  exit 1
fi

echo "passed"`) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ConfiguratorPrologueSh" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list `set -xe` `SERVICE_NAME=$1` `KUBERNETES_NODE_NAME=$2` `POD_ORDINAL=${SERVICE_NAME##*-}` "BROKER_INDEX=`expr $POD_ORDINAL + 1`" `` `CONFIG=/etc/redpanda/redpanda.yaml` `` `# Setup config files` `cp /tmp/base-config/redpanda.yaml "${CONFIG}"`)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.ConfiguratorRackAwarenessSh" -}}
{{- $nodeAnnotation := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list `` `# Configure Rack Awareness` `set +x` (printf `RACK=$(curl --silent --cacert /run/secrets/kubernetes.io/serviceaccount/ca.crt --fail -H 'Authorization: Bearer '$(cat /run/secrets/kubernetes.io/serviceaccount/token) "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT_HTTPS}/api/v1/nodes/${KUBERNETES_NODE_NAME}?pretty=true" | grep %s | grep -v '\"key\":' | sed 's/.*": "\([^"]\+\).*/\1/')` (printf `'%q'` $nodeAnnotation)) `set -x` `rpk --config "$CONFIG" redpanda config set redpanda.rack "${RACK}"`)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "_redpanda.WrapLifecycleHook" -}}
{{- $hook := (index .a 0) -}}
{{- $timeoutSeconds := (index .a 1) -}}
{{- $cmd := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $wrapped := (join " " $cmd) -}}
{{- $script := (printf (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" (printf "%s%s" `timeout -v %d %s 2>&1 | sed "s/^/lifecycle-hook %s $(date): /" | tee /proc/1/fd/1` "\n") `ec=${PIPESTATUS[0]}`) "\n") `if [ "$ec" = "124" ] || [ "$ec" = "137" ]; then`) "\n") `  echo "lifecycle-hook %s $(date): TIMEOUT after %ds — hook killed before completion; the broker will receive SIGTERM with work in-flight (exit $ec)" | tee /proc/1/fd/1`) "\n") `fi`) "\n") `true`) $timeoutSeconds $wrapped $hook $hook $timeoutSeconds) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list "bash" "-c" $script)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

