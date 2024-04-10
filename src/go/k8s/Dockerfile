# syntax=docker/dockerfile:1

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot as manager
ARG TARGETARCH
ARG TARGETOS
WORKDIR /
COPY redpanda-operator-${TARGETOS}-${TARGETARCH} ./manager
USER 65532:65532
ENTRYPOINT ["/manager"]

FROM gcr.io/distroless/static:nonroot as configurator
ARG TARGETARCH
ARG TARGETOS
WORKDIR /
COPY configurator-${TARGETOS}-${TARGETARCH} ./configurator
USER 65532:65532
ENTRYPOINT ["/configurator"]
