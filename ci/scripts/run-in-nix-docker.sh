#!/usr/bin/env bash

# Set -x (echo commands) so users can tell what's happening. Explicitly avoid
# -e because we're testing a lot of commands that will likely fail.
set -x

# When running docker/podman through a VM (Podman Desktop, colima, lima,
# DOCKER_HOST is set, etc), we need the path of the docker socket within that
# VM in order to provide docker API access within the container. It's a bit
# difficult to get that path 100% of the time but this should work in most
# cases:

# For Podman Desktop
PODMAN_MACHINE_SOCKET=$(podman machine ssh -- podman info -f '{{ .Host.RemoteSocket.Path }}')

# For colima
COLIMA_SOCKET=$(colima ssh -- docker context inspect -f '{{ .Endpoints.docker.Host }}')

# For docker running on the host (CI). We always want to fallback to this value
# LAST as it will always return something but will likely be wrong in the case
# of using a VM.
DOCKER_HOST_SOCKET=$(docker context inspect -f '{{ .Endpoints.docker.Host }}')

# Perform a bash version of "coalesce".
DOCKER_SOCKET=${PODMAN_MACHINE_SOCKET:-${COLIMA_SOCKET:-${DOCKER_HOST_SOCKET}}}

if [[ -z $DOCKER_SOCKET ]]; then
	echo 'Failed to detect docker socket.'
	exit 1
fi

# Spray and pray work is done, now we want to fail on errors.
set -e

# Build the base image and grab the SHA.
IMAGE_SHA=$(docker build --quiet -f ./ci/docker/nix.Dockerfile .)

# NB: --user 0:$(id -g) runs the nix process (and subproccesses) with the same
# group as the host. This allows the host (buildkite in most cases) to access
# build artifacts.
# The nix image in use doesn't work if run as a non-root user.
docker run --rm -it \
	-e DOCKER_HOST=unix:///var/run/docker.sock \
	-e PWD \
	-e CI \
	-e COMMIT \
	-e BRANCH_NAME \
	-e TAG_NAME \
	-e PULL_REQUEST \
	-e PULL_REQUEST_BASE_BRANCH \
	-e DOCKERHUB_USER \
	-e DOCKERHUB_TOKEN \
	-e CLOUDSMITH_USERNAME \
	-e CLOUDSMITH_API_KEY \
	-e GITHUB_API_TOKEN \
	-e RPK_TEST_CLIENT_ID \
	-e RPK_TEST_CLIENT_SECRET \
	--user 0:$(id -g) \
	--privileged \
	--net=host \
	--ipc=host \
	--volume ${DOCKER_SOCKET:7}:/var/run/docker.sock \
	--volume $(pwd):/work \
	$IMAGE_SHA "$@"
