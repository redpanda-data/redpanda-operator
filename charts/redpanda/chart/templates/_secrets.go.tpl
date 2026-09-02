{{- /* GENERATED FILE DO NOT EDIT */ -}}
{{- /* Transpiled by gotohelm from "github.com/redpanda-data/redpanda-operator/charts/redpanda/v25/secrets.go" */ -}}

{{- define "redpanda.Secrets" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $secrets := (coalesce nil) -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretSTSLifecycle" (dict "a" (list $state)))) "r"))) -}}
{{- $saslUsers_1 := (get (fromJson (include "redpanda.SecretSASLUsers" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $saslUsers_1) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $saslUsers_1)) -}}
{{- end -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretConfigurator" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "replicas" 0 "updateStrategy" (dict) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0) "sideCars" (dict "image" (dict "repository" "" "tag" "") "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "")) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)) (0 | int))))) "r"))) -}}
{{- $fsValidator_2 := (get (fromJson (include "redpanda.SecretFSValidator" (dict "a" (list $state (mustMergeOverwrite (dict "Name" "" "Generation" "" "Statefulset" (dict "replicas" 0 "updateStrategy" (dict) "podTemplate" (dict) "budget" (dict "maxUnavailable" 0) "podAntiAffinity" (dict "topologyKey" "" "type" "" "weight" 0) "sideCars" (dict "image" (dict "repository" "" "tag" "") "pvcUnbinder" (dict "enabled" false "unbindAfter" "" "disableStuckClaimExemption" false) "brokerDecommissioner" (dict "enabled" false "decommissionAfter" "" "decommissionRequeueTimeout" "") "configWatcher" (dict "enabled" false) "rpkProfileWatcher" (dict "enabled" false) "controllers" (dict "enabled" false "createRBAC" false "healthProbeAddress" "" "metricsAddress" "" "pprofAddress" "")) "initContainers" (dict "fsValidator" (dict "enabled" false "expectedFS" "") "setDataDirOwnership" (dict "enabled" false) "configurator" (dict)) "initContainerImage" (dict "repository" "" "tag" "")) "ServiceAnnotations" (coalesce nil)) (dict "Statefulset" $state.Values.statefulset)))))) "r") -}}
{{- if (ne (toJson $fsValidator_2) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $fsValidator_2)) -}}
{{- end -}}
{{- $ordinalOffset := (($state.Values.statefulset.replicas | int) | int) -}}
{{- range $_, $set := $state.Pools -}}
{{- $secrets = (concat (default (list) $secrets) (list (get (fromJson (include "redpanda.SecretConfigurator" (dict "a" (list $state $set $ordinalOffset)))) "r"))) -}}
{{- $fsValidator_3 := (get (fromJson (include "redpanda.SecretFSValidator" (dict "a" (list $state $set)))) "r") -}}
{{- if (ne (toJson $fsValidator_3) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $fsValidator_3)) -}}
{{- end -}}
{{- $ordinalOffset = ((add $ordinalOffset (($set.Statefulset.replicas | int) | int)) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $bootstrapUser_4 := (get (fromJson (include "redpanda.SecretBootstrapUser" (dict "a" (list $state)))) "r") -}}
{{- if (ne (toJson $bootstrapUser_4) "null") -}}
{{- $secrets = (concat (default (list) $secrets) (list $bootstrapUser_4)) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secrets) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretSTSLifecycle" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $replicas := ($state.Values.statefulset.replicas | int) -}}
{{- range $_, $set := $state.Pools -}}
{{- $replicas = ((add $replicas ($set.Statefulset.replicas | int)) | int) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $adminCurlFlags := (get (fromJson (include "redpanda.adminTLSCurlFlags" (dict "a" (list $state)))) "r") -}}
{{- $drain := (and (gt $replicas (2 | int)) (not (get (fromJson (include "_shims.typeassertion" (dict "a" (list "bool" (dig "recovery_mode_enabled" false $state.Values.config.node))))) "r"))) -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%s-sts-lifecycle" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict "common.sh" (get (fromJson (include "redpanda.LifecycleCommonSh" (dict "a" (list (get (fromJson (include "redpanda.adminInternalURL" (dict "a" (list $state)))) "r") $adminCurlFlags (printf "${SERVICE_NAME}.%s" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")))))) "r") "postStart.sh" (get (fromJson (include "redpanda.LifecyclePostStartSh" (dict "a" (list $adminCurlFlags)))) "r") "preStop.sh" (get (fromJson (include "redpanda.LifecyclePreStopSh" (dict "a" (list $adminCurlFlags $drain)))) "r")))) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.LifecycleCommonSh" -}}
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

{{- define "redpanda.LifecyclePostStartSh" -}}
{{- $adminCurlFlags := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (join "\n" (list `#!/usr/bin/env bash` `# This code should be similar if not exactly the same as that found in the panda-operator, see` `# https://github.com/redpanda-data/redpanda/blob/e51d5b7f2ef76d5160ca01b8c7a8cf07593d29b6/src/go/k8s/pkg/resources/secret.go` `` `# path below should match the path defined on the statefulset` `source /var/lifecycle/common.sh` `` `postStartHook () {` `  set -x` `` `  touch /tmp/postStartHookStarted` `` `  until NODE_ID=$(${CURL_NODE_ID_CMD} | grep -o '\"node_id\":[^,}]*' | grep -o '[^: ]*$'); do` `      sleep 0.5` `  done` `` `  echo "Clearing maintenance mode on node ${NODE_ID}"` (printf `  CURL_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${NODE_ID}/maintenance"` $adminCurlFlags) `  # a 400 here would mean not in maintenance mode` `  until [ "${status:-}" = '"200"' ] || [ "${status:-}" = '"400"' ]; do` `      status=$(${CURL_MAINTENANCE_DELETE_CMD})` `      sleep 0.5` `  done` `` `  # A previous incarnation of this pod may have re-registered with the` `  # cluster under a different node id after losing its data directory` `  # (e.g. when running without persistent storage). preStop put that old` `  # node id into maintenance mode and nothing ever clears it: the ghost` `  # broker occupies the cluster's only maintenance-mode slot, and the` `  # partition autobalancer excludes maintenance-mode brokers from` `  # auto-decommission (redpanda-data/redpanda-operator#1674). Clear` `  # maintenance mode on any broker id that advertises this pod's address` `  # but isn't the current broker. Only a broker the cluster reports dead` `  # (is_alive=false) and draining is cleared — but the health monitor can` `  # lag a fast pod restart and briefly report the dead previous` `  # incarnation as still alive, so keep re-checking until every stale` `  # drainer at this address is gone; the surrounding timeout wrapper` `  # bounds this loop.` `  while true; do` `      GHOSTS_REMAIN=false` `      until BROKER_IDS=$(${CURL_BROKERS_CMD_PREFIX} | grep -o '\"node_id\": *[0-9-]*' | grep -o '[0-9-]*$'); do` `          sleep 0.5` `      done` `      for BROKER_ID in ${BROKER_IDS}; do` `          if [ "${BROKER_ID}" = "${NODE_ID}" ]; then continue; fi` `          BROKER=$(${CURL_BROKERS_CMD_PREFIX}/${BROKER_ID}) || continue` `          # compare the advertised address exactly (not as a regex, which` `          # would treat the dots in ${POD_FQDN} as wildcards)` `          BROKER_ADDRESS=$(echo "${BROKER}" | grep -o '\"internal_rpc_address\": *\"[^\"]*' | grep -o '[^\"]*$')` `          if [ "${BROKER_ADDRESS}" != "${POD_FQDN}" ]; then continue; fi` `          if ! echo "${BROKER}" | grep -q '\"draining\": *true'; then continue; fi` `          # a drainer at this pod's address still reported alive is the` `          # health monitor lagging behind the restart; retry until the` `          # cluster reports it dead` `          if ! echo "${BROKER}" | grep -q '\"is_alive\": *false'; then GHOSTS_REMAIN=true; continue; fi` `          echo "Clearing maintenance mode left behind by node ${BROKER_ID}, a previous incarnation of this pod"` (printf `          CURL_GHOST_MAINTENANCE_DELETE_CMD="${CURL_MAINTENANCE_DELETE_CMD_PREFIX} %s ${CURL_URL}/v1/brokers/${BROKER_ID}/maintenance"` $adminCurlFlags) `          GHOST_STATUS=""` `          # a 400 means not in maintenance mode, a 404 means already removed` `          until [ "${GHOST_STATUS:-}" = '"200"' ] || [ "${GHOST_STATUS:-}" = '"400"' ] || [ "${GHOST_STATUS:-}" = '"404"' ]; do` `              GHOST_STATUS=$(${CURL_GHOST_MAINTENANCE_DELETE_CMD})` `              sleep 0.5` `          done` `      done` `      if [ "${GHOSTS_REMAIN}" = "false" ]; then break; fi` `      sleep 5` `  done` `` `  touch /tmp/postStartHookFinished` `}` `` `postStartHook` `true`))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.LifecyclePreStopSh" -}}
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

{{- define "redpanda.SecretSASLUsers" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (and (ne $state.Values.auth.sasl.secretRef "") $state.Values.auth.sasl.enabled) (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.auth.sasl.users)))) "r") | int) (0 | int))) -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $state.Values.auth.sasl.secretRef "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict))) -}}
{{- $usersTxt := (list) -}}
{{- $defaultMechanism := "SCRAM-SHA-512" -}}
{{- if (ne $state.Values.auth.sasl.mechanism "") -}}
{{- $defaultMechanism = $state.Values.auth.sasl.mechanism -}}
{{- end -}}
{{- range $_, $user := $state.Values.auth.sasl.users -}}
{{- $mechanism := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $user.mechanism $defaultMechanism)))) "r") -}}
{{- $usersTxt = (concat (default (list) $usersTxt) (list (printf "%s:%s:%s" $user.name $user.password $mechanism))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $_ := (set $secret.stringData "users.txt" (join "\n" $usersTxt)) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- else -}}{{- if (and $state.Values.auth.sasl.enabled (eq $state.Values.auth.sasl.secretRef "")) -}}
{{- $_ := (fail "auth.sasl.secretRef cannot be empty when auth.sasl.enabled=true") -}}
{{- else -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretBootstrapUser" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (or (not $state.Values.auth.sasl.enabled) (ne (toJson $state.Values.auth.sasl.bootstrapUser.secretKeyRef) "null")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secretName := (printf "%s-bootstrap-user" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r")) -}}
{{- if (ne (toJson $state.BootstrapUserSecret) "null") -}}
{{- $_is_returning = true -}}
{{- (dict "r" $state.BootstrapUserSecret) | toJson -}}
{{- break -}}
{{- end -}}
{{- $password := (randAlphaNum (32 | int)) -}}
{{- $userPassword := $state.Values.auth.sasl.bootstrapUser.password -}}
{{- if (ne (toJson $userPassword) "null") -}}
{{- $password = $userPassword -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" $secretName "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "immutable" true "type" "Opaque" "stringData" (dict "password" $password)))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.SecretFSValidator" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not $pool.Statefulset.initContainers.fsValidator.enabled) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (coalesce nil)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $secret := (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%.49s-fs-validator" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r"))) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict))) -}}
{{- $_ := (set $secret.stringData "fsValidator.sh" (get (fromJson (include "redpanda.FSValidatorSh" (dict "a" (list)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" $secret) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.FSValidatorSh" -}}
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

{{- define "redpanda.SecretConfigurator" -}}
{{- $state := (index .a 0) -}}
{{- $pool := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $configuratorSh := (get (fromJson (include "redpanda.ConfiguratorPrologueSh" (dict "a" (list)))) "r") -}}
{{- $kafkaSnippet := (get (fromJson (include "redpanda.secretConfiguratorKafkaConfig" (dict "a" (list $state $pool.Statefulset $ordinalOffset)))) "r") -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) $kafkaSnippet)) -}}
{{- $httpSnippet := (get (fromJson (include "redpanda.secretConfiguratorHTTPConfig" (dict "a" (list $state $pool.Statefulset $ordinalOffset)))) "r") -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) $httpSnippet)) -}}
{{- if $state.Values.rackAwareness.enabled -}}
{{- $configuratorSh = (concat (default (list) $configuratorSh) (default (list) (get (fromJson (include "redpanda.ConfiguratorRackAwarenessSh" (dict "a" (list $state.Values.rackAwareness.nodeAnnotation)))) "r"))) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" (mustMergeOverwrite (dict "metadata" (dict)) (mustMergeOverwrite (dict) (dict "apiVersion" "v1" "kind" "Secret")) (dict "metadata" (mustMergeOverwrite (dict) (dict "name" (printf "%.51s-configurator" (printf "%s%s" (get (fromJson (include "redpanda.Fullname" (dict "a" (list $state)))) "r") (get (fromJson (include "redpanda.Pool.Suffix" (dict "a" (list (deepCopy $pool))))) "r"))) "namespace" $state.Release.Namespace "labels" (get (fromJson (include "redpanda.FullLabels" (dict "a" (list $state)))) "r"))) "type" "Opaque" "stringData" (dict "configurator.sh" (join "\n" $configuratorSh))))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ConfiguratorPrologueSh" -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list `set -xe` `SERVICE_NAME=$1` `KUBERNETES_NODE_NAME=$2` `POD_ORDINAL=${SERVICE_NAME##*-}` "BROKER_INDEX=`expr $POD_ORDINAL + 1`" `` `CONFIG=/etc/redpanda/redpanda.yaml` `` `# Setup config files` `cp /tmp/base-config/redpanda.yaml "${CONFIG}"`)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.ConfiguratorRackAwarenessSh" -}}
{{- $nodeAnnotation := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (list `` `# Configure Rack Awareness` `set +x` (printf `RACK=$(curl --silent --cacert /run/secrets/kubernetes.io/serviceaccount/ca.crt --fail -H 'Authorization: Bearer '$(cat /run/secrets/kubernetes.io/serviceaccount/token) "https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT_HTTPS}/api/v1/nodes/${KUBERNETES_NODE_NAME}?pretty=true" | grep %s | grep -v '\"key\":' | sed 's/.*": "\([^"]\+\).*/\1/')` (printf `'%q'` $nodeAnnotation)) `set -x` `rpk --config "$CONFIG" redpanda config set redpanda.rack "${RACK}"`)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.secretConfiguratorKafkaConfig" -}}
{{- $state := (index .a 0) -}}
{{- $sts := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $internalAdvertiseAddress := (printf "%s.%s" "${SERVICE_NAME}" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) -}}
{{- $snippet := (coalesce nil) -}}
{{- $listenerName := "kafka" -}}
{{- $listenerAdvertisedName := $listenerName -}}
{{- $redpandaConfigPart := "redpanda" -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `LISTENER=%q` (toJson (dict "name" "internal" "address" $internalAdvertiseAddress "port" ($state.Values.listeners.kafka.port | int)))) (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"` $redpandaConfigPart $listenerAdvertisedName))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.listeners.kafka.external)))) "r") | int) (0 | int)) -}}
{{- $externalCounter := (0 | int) -}}
{{- range $externalName, $externalVals := $state.Values.listeners.kafka.external -}}
{{- $externalCounter = ((add $externalCounter (1 | int)) | int) -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `ADVERTISED_%s_ADDRESSES=()` (upper $listenerName)))) -}}
{{- range $_, $replicaIndex := (until (($sts.replicas | int) | int)) -}}
{{- $port := ($externalVals.port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $externalVals.advertisedPorts (0 | int)) -}}
{{- else -}}
{{- $port = (index $externalVals.advertisedPorts $replicaIndex) -}}
{{- end -}}
{{- end -}}
{{- $host := (get (fromJson (include "redpanda.advertisedHostJSON" (dict "a" (list $state $externalName $port $replicaIndex ((add $ordinalOffset $replicaIndex) | int) (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.hostTemplate "")))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $externalVals)))) "r"))))) "r") -}}
{{- $address := (toJson $host) -}}
{{- $prefixTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.prefixTemplate "")))) "r") -}}
{{- if (eq $prefixTemplate "") -}}
{{- $prefixTemplate = (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.prefixTemplate "")))) "r") -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `PREFIX_TEMPLATE=%q` $prefixTemplate) (printf `ADVERTISED_%s_ADDRESSES+=(%q)` (upper $listenerName) $address))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"` $redpandaConfigPart $listenerAdvertisedName $externalCounter (upper $listenerName)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $snippet) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.secretConfiguratorHTTPConfig" -}}
{{- $state := (index .a 0) -}}
{{- $sts := (index .a 1) -}}
{{- $ordinalOffset := (index .a 2) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $internalAdvertiseAddress := (printf "%s.%s" "${SERVICE_NAME}" (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) -}}
{{- $snippet := (coalesce nil) -}}
{{- $listenerName := "http" -}}
{{- $listenerAdvertisedName := "pandaproxy" -}}
{{- $redpandaConfigPart := "pandaproxy" -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `LISTENER=%q` (toJson (dict "name" "internal" "address" $internalAdvertiseAddress "port" ($state.Values.listeners.http.port | int)))) (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[0] "$LISTENER"` $redpandaConfigPart $listenerAdvertisedName))) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.listeners.http.external)))) "r") | int) (0 | int)) -}}
{{- $externalCounter := (0 | int) -}}
{{- range $externalName, $externalVals := $state.Values.listeners.http.external -}}
{{- $externalCounter = ((add $externalCounter (1 | int)) | int) -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `ADVERTISED_%s_ADDRESSES=()` (upper $listenerName)))) -}}
{{- range $_, $replicaIndex := (until (($sts.replicas | int) | int)) -}}
{{- $port := ($externalVals.port | int) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (0 | int)) -}}
{{- if (eq ((get (fromJson (include "_shims.len" (dict "a" (list $externalVals.advertisedPorts)))) "r") | int) (1 | int)) -}}
{{- $port = (index $externalVals.advertisedPorts (0 | int)) -}}
{{- else -}}
{{- $port = (index $externalVals.advertisedPorts $replicaIndex) -}}
{{- end -}}
{{- end -}}
{{- $host := (get (fromJson (include "redpanda.advertisedHostJSON" (dict "a" (list $state $externalName $port $replicaIndex ((add $ordinalOffset $replicaIndex) | int) (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.host "")))) "r") (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.hostTemplate "")))) "r") (get (fromJson (include "redpanda.ExternalListener.IsGatewayListener" (dict "a" (list $externalVals)))) "r"))))) "r") -}}
{{- $address := (toJson $host) -}}
{{- $prefixTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $externalVals.prefixTemplate "")))) "r") -}}
{{- if (eq $prefixTemplate "") -}}
{{- $prefixTemplate = (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.prefixTemplate "")))) "r") -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `PREFIX_TEMPLATE=%q` $prefixTemplate) (printf `ADVERTISED_%s_ADDRESSES+=(%q)` (upper $listenerName) $address))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- $snippet = (concat (default (list) $snippet) (list `` (printf `rpk redpanda config --config "$CONFIG" set %s.advertised_%s_api[%d] "${ADVERTISED_%s_ADDRESSES[$POD_ORDINAL]}"` $redpandaConfigPart $listenerAdvertisedName $externalCounter (upper $listenerName)))) -}}
{{- end -}}
{{- if $_is_returning -}}
{{- break -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $snippet) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminTLSCurlFlags" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (not (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r")) -}}
{{- $_is_returning = true -}}
{{- (dict "r" "") | toJson -}}
{{- break -}}
{{- end -}}
{{- if $state.Values.listeners.admin.tls.requireClientAuth -}}
{{- $path := (get (fromJson (include "redpanda.InternalTLS.ClientMountPoint" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s/ca.crt --cert %s/tls.crt --key %s/tls.key" $path $path $path)) | toJson -}}
{{- break -}}
{{- end -}}
{{- $path := (get (fromJson (include "redpanda.InternalTLS.ServerCAPath" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "--cacert %s" $path)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.externalAdvertiseAddress" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $eaa := "${SERVICE_NAME}" -}}
{{- $externalDomainTemplate := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") -}}
{{- $expanded := (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $externalDomainTemplate))))) "r") -}}
{{- if (ne ((get (fromJson (include "_shims.len" (dict "a" (list $expanded)))) "r") | int) (0 | int)) -}}
{{- $eaa = (printf "%s.%s" "${SERVICE_NAME}" $expanded) -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $eaa) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedHostJSON" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $port := (index .a 2) -}}
{{- $replicaIndex := (index .a 3) -}}
{{- $globalOrdinal := (index .a 4) -}}
{{- $host := (index .a 5) -}}
{{- $hostTemplate := (index .a 6) -}}
{{- $isGateway := (index .a 7) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (and (get (fromJson (include "redpanda.ExternalConfig.IsGatewayEnabled" (dict "a" (list $state.Values.external)))) "r") $isGateway) -}}
{{- $_is_returning = true -}}
{{- (dict "r" (get (fromJson (include "redpanda.advertisedHostJSONGateway" (dict "a" (list $state $name $globalOrdinal $host $hostTemplate)))) "r")) | toJson -}}
{{- break -}}
{{- end -}}
{{- $hostMap := (dict "name" $name "address" (get (fromJson (include "redpanda.externalAdvertiseAddress" (dict "a" (list $state)))) "r") "port" $port) -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (0 | int)) -}}
{{- $address := "" -}}
{{- if (gt ((get (fromJson (include "_shims.len" (dict "a" (list $state.Values.external.addresses)))) "r") | int) (1 | int)) -}}
{{- $address = (index $state.Values.external.addresses $replicaIndex) -}}
{{- else -}}
{{- $address = (index $state.Values.external.addresses (0 | int)) -}}
{{- end -}}
{{- $domain_5 := (get (fromJson (include "_shims.ptr_Deref" (dict "a" (list $state.Values.external.domain "")))) "r") -}}
{{- if (ne $domain_5 "") -}}
{{- $hostMap = (dict "name" $name "address" (printf "%s.%s" $address (get (fromJson (include (first $state.Template) (dict "a" (concat (rest $state.Template) (list $domain_5))))) "r")) "port" $port) -}}
{{- else -}}
{{- $hostMap = (dict "name" $name "address" $address "port" $port) -}}
{{- end -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" $hostMap) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.advertisedHostJSONGateway" -}}
{{- $state := (index .a 0) -}}
{{- $name := (index .a 1) -}}
{{- $globalOrdinal := (index .a 2) -}}
{{- $host := (index .a 3) -}}
{{- $hostTemplate := (index .a 4) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $gw := $state.Values.external.gateway -}}
{{- $port := ((get (fromJson (include "redpanda.GatewayConfig.GatewayAdvertisedPort" (dict "a" (list $gw)))) "r") | int) -}}
{{- if (eq $hostTemplate "") -}}
{{- $hostTemplate = $host -}}
{{- end -}}
{{- $pods := (get (fromJson (include "redpanda.gatewayPodNames" (dict "a" (list $state)))) "r") -}}
{{- $podName := "" -}}
{{- if (lt $globalOrdinal ((get (fromJson (include "_shims.len" (dict "a" (list $pods)))) "r") | int)) -}}
{{- $podName = (index $pods $globalOrdinal) -}}
{{- end -}}
{{- $address := (get (fromJson (include "redpanda.renderBrokerHost" (dict "a" (list $hostTemplate $globalOrdinal $podName)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" (dict "name" $name "address" $address "port" $port)) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminInternalHTTPProtocol" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- if (get (fromJson (include "redpanda.InternalTLS.IsEnabled" (dict "a" (list $state.Values.listeners.admin.tls $state.Values.tls)))) "r") -}}
{{- $_is_returning = true -}}
{{- (dict "r" "https") | toJson -}}
{{- break -}}
{{- end -}}
{{- $_is_returning = true -}}
{{- (dict "r" "http") | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

{{- define "redpanda.adminInternalURL" -}}
{{- $state := (index .a 0) -}}
{{- range $_ := (list 1) -}}
{{- $_is_returning := false -}}
{{- $_is_returning = true -}}
{{- (dict "r" (printf "%s://%s.%s:%d" (get (fromJson (include "redpanda.adminInternalHTTPProtocol" (dict "a" (list $state)))) "r") `${SERVICE_NAME}` (trimSuffix "." (get (fromJson (include "redpanda.InternalDomain" (dict "a" (list $state)))) "r")) ($state.Values.listeners.admin.port | int))) | toJson -}}
{{- break -}}
{{- end -}}
{{- end -}}

