#!/bin/bash

# This script requires having configured the bk CLI to use the redpanda organization and
# the token in the environment variable BUILDKITE_GRAPHQL_TOKEN. In addition to the above
# environment variable, it also requires the BASIC credentials for downloading artifacts
# from Buildkite as ARTIFACTS_DOWNLOAD_USER and ARTIFACTS_DOWNLOAD_PASSWORD.
# 
# It works in two modes, either passing $BRANCH $ARTIFACT_PREFIX or just $ARTIFACT_PREFIX
# to target the latest build on the current branch. All files are output to an artifacts
# directory.
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
BRANCH="$1"
ARTIFACTS="$1"
BRANCH_DESCRIPTION="the current branch"

if [ -z "$2" ]; then
BRANCH=""
else
ARTIFACTS="$2"
fi

if [ -z "$BRANCH" ]; then
BRANCH_ARG=""
else
BRANCH_ARG="--branch $BRANCH"
BRANCH_DESCRIPTION="Branch: $BRANCH"
fi

mkdir -p artifacts
BUILD=$(bk build view $BRANCH_ARG 2> /dev/null)
LATEST_BUILD=$(echo $BUILD | head -n 1 | xargs | cut -d' ' -f2 | sed 's/^.//')
echo "Downloading OTEL artifacts for ${BRANCH_DESCRIPTION}, Build: ${LATEST_BUILD}, Matching: ${ARTIFACTS}"
bk api /pipelines/redpanda-operator/builds/${LATEST_BUILD}/artifacts | jq ".[] | select((.filename | startswith(\"${ARTIFACTS}.test-\")) and (.file_size != 0)) | .download_url" -r |
while read -r line
do
    FIRST_REDIRECT=$(curl -s -H "Authorization: Bearer $TOKEN" $line | jq .url -r)
    SECOND_REDIRECT=$(curl -si -u $BASIC_USER:$BASIC_PASSWORD $FIRST_REDIRECT | grep 'Location:' | cut -d' ' -f2 | tr -dc '[[:print:]]')
    FILE=$(basename $SECOND_REDIRECT)
    echo "Downloading: artifacts/$FILE"
    curl -s -o artifacts/"$FILE" -u $BASIC_USER:$BASIC_PASSWORD $SECOND_REDIRECT
done