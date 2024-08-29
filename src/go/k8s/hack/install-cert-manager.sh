#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

CERT_MANAGER_VERSION="v1.13.3"

# the recommended approach using a static manifest:
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml
kubectl -n cert-manager wait --for=condition=Available deploy/cert-manager-cainjector deploy/cert-manager-webhook deploy/cert-manager
