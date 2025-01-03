# syntax=docker/dockerfile:1

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot AS manager
ARG TARGETARCH
ARG TARGETOS
WORKDIR /
USER 65532:65532

# For backwards compatibility with deployments that specify the
# entrypoint/command.
COPY manager-${TARGETOS}-${TARGETARCH} /manager
COPY redpanda-operator-${TARGETOS}-${TARGETARCH} /redpanda-operator
ENTRYPOINT ["/redpanda-operator", "run"]
