#!/usr/bin/env bash

set -eu

if [[ -n "${BUILDKITE_TAG-}" ]]; then
  echo "$BUILDKITE_TAG"
elif [[ "${NIGHTLY_K8S:-}" == "1" ]] || [[ "${NIGHTLY_RELEASE:-}" == "true" ]]; then
  echo "v0.0.0-$(date --utc +%Y%m%d)git$(git rev-parse --short=7 HEAD)"
fi
