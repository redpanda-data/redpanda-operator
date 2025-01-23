#!/usr/bin/env bash

set -eu

if [[ -n "${BUILDKITE_TAG-}" ]]; then
  case "${BUILDKITE_TAG-}" in
    # Git tags that starts like charts go module are not considered release tags that would
    # trigger operator release process.
    #
    # As buildkite has separated configuration "Branch Limiting" which is set to `main` and
    # `v*` this `case` is a noop.
    charts/connectors/v* | charts/console/v* | charts/operator/v* | charts/redpanda/v*)
      echo ""
      ;;
    *)
      echo "$BUILDKITE_TAG"
      ;;
  esac
elif [[ "${NIGHTLY_K8S:-}" == "1" ]] || [[ "${NIGHTLY_RELEASE:-}" == "true" ]]; then
  echo "v0.0.0-$(date --utc +%Y%m%d)git$(git rev-parse --short=7 HEAD)"
fi
