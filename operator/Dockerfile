# syntax=docker/dockerfile:1

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot AS manager
ARG TARGETARCH
ARG TARGETOS
WORKDIR /
USER 65532:65532

COPY redpanda-operator-${TARGETOS}-${TARGETARCH} /redpanda-operator

# No `ln` in rootless so we build an "alias" binary that exec's
# "/bin/redpanda-operator run" for backwards compatibility with deployments
# that still specify old entrypoints/commands.
COPY alias-${TARGETOS}-${TARGETARCH} /manager

ENTRYPOINT ["redpanda-operator", "run"]
