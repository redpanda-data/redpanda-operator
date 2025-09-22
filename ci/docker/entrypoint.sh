#!/usr/bin/env bash

# NB: Only our CI agents push to this cache but we don't have PKI (e.g.
# signing) setup for them.
## So we mark the cache as trusted to skip signature verifications.
CACHE="s3://redpanda-nix-cache-k8s-m6id12xlarge?region=us-west-2&trusted=true"

nix develop --option extra-substituters "$CACHE" --impure --command "$@"
EXIT_CODE="$?"

echo "--- Pushing to :nix: cache"
nix copy --to "$CACHE" .#devshell

exit "$EXIT_CODE"
