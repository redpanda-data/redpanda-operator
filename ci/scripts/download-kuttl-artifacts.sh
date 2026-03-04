#!/bin/bash

# This script requires having configured the bk CLI to use the redpanda organization and
# the token in the environment variable BUILDKITE_GRAPHQL_TOKEN. In addition to the above
# environment variable, it also requires the BASIC credentials for downloading artifacts
# from Buildkite as ARTIFACTS_DOWNLOAD_USER and ARTIFACTS_DOWNLOAD_PASSWORD.
#
# Usage: download-kuttl-artifacts.sh [BRANCH]
#
# Pass a branch name to target a specific branch, or omit to use the current branch.
# All files are output to an artifacts directory.
#
# The Buildkite token must have the GraphQL api enabled and requires the following scopes:
# - read_artifacts
# - read_builds
# - read_build_logs
# - read_organizations
# - read_pipelines
# - read_user

TOKEN="${BUILDKITE_GRAPHQL_TOKEN}"
BASIC_USER="${ARTIFACTS_DOWNLOAD_USER}"
BASIC_PASSWORD="${ARTIFACTS_DOWNLOAD_PASSWORD}"
BRANCH="${1}"
BRANCH_DESCRIPTION="the current branch"
ARTIFACTS="_e2e_with_flags_artifacts"

if [ -n "$BRANCH" ]; then
BRANCH_ARG="--branch $BRANCH"
BRANCH_DESCRIPTION="Branch: $BRANCH"
else
BRANCH_ARG=""
fi

mkdir -p artifacts
BUILD=$(bk build view $BRANCH_ARG 2> /dev/null)
LATEST_BUILD=$(echo $BUILD | head -n 1 | xargs | cut -d' ' -f2 | sed 's/^.//')
echo "Downloading kuttl artifacts for ${BRANCH_DESCRIPTION}, Build: ${LATEST_BUILD}, Matching Prefix: ${ARTIFACTS}"
bk api /pipelines/redpanda-operator/builds/${LATEST_BUILD}/artifacts | jq ".[] | select((.filename | startswith(\"${ARTIFACTS}\")) and (.filename | endswith(\".tar.gz\")) and (.file_size != 0)) | .download_url" -r |
while read -r line
do
    DOWNLOAD_URL=$(curl -s -H "Authorization: Bearer $TOKEN" $line | jq .url -r)
    FILE=$(basename "$DOWNLOAD_URL")
    echo "Downloading: artifacts/$FILE"
    curl -sL -o artifacts/"$FILE" -u $BASIC_USER:$BASIC_PASSWORD "$DOWNLOAD_URL"
done
