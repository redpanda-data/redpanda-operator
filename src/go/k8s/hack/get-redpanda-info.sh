#!/usr/bin/env bash

set -x

script_namespace=${1:-redpanda-system}
test_directory=${2:-../../_e2e_artifacts}

ARTIFACTS_PATH=$(TMPDIR=$test_directory mktemp -d)

mkdir -p $ARTIFACTS_PATH/exported-logs/
kind export logs --name kind $ARTIFACTS_PATH/exported-logs/

kubectl -n $script_namespace get pods -o yaml >$ARTIFACTS_PATH/pods.yaml

for cl in $(kubectl -n $script_namespace get cluster --output=jsonpath='{.items..metadata.name}'); do
  replication_factor=$(kubectl -n $script_namespace get cluster $cl --output=jsonpath='{.spec.replicas}')

  tls_enabled=$(kubectl -n $script_namespace get cluster $cl --output=jsonpath='{.spec.configuration.adminApi[0].tls.enabled}')
  curl_arguments="-s http"
  if [[ $tls_enabled == "true" ]]; then
    curl_arguments="-sk https"
  fi

  mtls_enabled=$(kubectl -n $script_namespace get cluster $cl --output=jsonpath='{.spec.configuration.adminApi[0].tls.requireClientAuth}')
  if [[ $mtls_enabled == "true" ]]; then
    curl_arguments="-sk --cert /etc/tls/certs/admin/tls.crt --key /etc/tls/certs/admin/tls.key https"
  fi

  kubectl -n $script_namespace get cluster $cl -o yaml >$ARTIFACTS_PATH/$cl.yaml

  kubectl -n $script_namespace get svc -A -o yaml >$ARTIFACTS_PATH/svc-all-for-port-collision.yaml

  i=0
  while [[ $i -lt $replication_factor ]]; do
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/brokers >$ARTIFACTS_PATH/brokers-from-pod-$cl-$i.json || true
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/brokers/$i >$ARTIFACTS_PATH/broker-$i-from-pod-$cl-$i.json || true
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/node_config >$ARTIFACTS_PATH/node-config-from-pod-$cl-$i.json || true
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/cluster_config/status >$ARTIFACTS_PATH/config-status-from-pod-$cl-$i.json || true
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/status/ready >$ARTIFACTS_PATH/status-ready-pod-$cl-$i.json || true
    kubectl -n $script_namespace exec -c redpanda $cl-$i -- curl $curl_arguments://$cl-$i.$cl.$script_namespace.svc.cluster.local.:9644/v1/features >$ARTIFACTS_PATH/features-from-pod-$cl-$i.json || true
    kubectl -n $script_namespace logs -c redpanda $cl-$i >$ARTIFACTS_PATH/logs-from-pod-$cl-$i.txt || true
    kubectl -n $script_namespace logs -c redpanda $cl-$i -p >$ARTIFACTS_PATH/logs-from-previous-pod-$cl-$i.txt || true
    ((i = i + 1))
  done
done

kubectl get -n $script_namespace certificates -o yaml >$ARTIFACTS_PATH/certificates.yaml
kubectl get -n $script_namespace certificatesigningrequests -o yaml >$ARTIFACTS_PATH/certificatesigningrequests.yaml
kubectl get -n $script_namespace issuers -o yaml >$ARTIFACTS_PATH/issuers.yaml
kubectl get clusterissuers -o yaml >$ARTIFACTS_PATH/all-clusterissuers.yaml
kubectl get -n $script_namespace certificaterequests -o yaml >$ARTIFACTS_PATH/certificaterequests.yaml

kubectl get -n $script_namespace sts -o yaml >$ARTIFACTS_PATH/sts.yaml
kubectl get -n $script_namespace redpanda -o yaml >$ARTIFACTS_PATH/redpandas.yaml

kubectl get events -n $script_namespace --sort-by metadata.creationTimestamp >$ARTIFACTS_PATH/events.txt
kubectl get events --sort-by metadata.creationTimestamp -A >$ARTIFACTS_PATH/all-events.txt
kubectl describe node >$ARTIFACTS_PATH/described-nodes.txt
kubectl get pod -A -o yaml >$ARTIFACTS_PATH/all-pods.yaml
