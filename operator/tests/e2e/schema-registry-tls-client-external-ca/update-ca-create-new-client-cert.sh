#!/usr/bin/env bash

set -e

KEY_FILE=/tmp/client.key.$$
CRT_FILE=/tmp/client.crt.$$
CN=e2e-test-schema-client1
SECRET_NAME="sr-client-cert-key1"

# Create self signed cert and key
openssl req -x509 -newkey rsa:2048 -sha256 -days 36500 -nodes -keyout $KEY_FILE -out $CRT_FILE -subj "/CN=$CN.redpanda.com"
kubectl create -n $NAMESPACE secret generic $SECRET_NAME --from-file=tls\.key=$KEY_FILE --from-file=tls\.crt=$CRT_FILE

# Update the external CA cert secret with the new self signed CA
SECRET_NAME="sr-ca-cert"
kubectl create -n $NAMESPACE secret generic $SECRET_NAME --from-file=ca\.crt=$CRT_FILE --save-config --dry-run=client -o yaml | kubectl apply -f -

rm $KEY_FILE $CRT_FILE
