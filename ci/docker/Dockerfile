FROM public.ecr.aws/docker/library/buildpack-deps:jammy-scm
ARG DEBIAN_FRONTEND=noninteractive

# gcc for cgo
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      g++ \
      gcc \
      libc6-dev \
      make \
      pkg-config && \
    rm -rf /var/lib/apt/lists/*

# docker client and buildx plugin
RUN curl -fsSLO https://download.docker.com/linux/static/stable/x86_64/docker-20.10.17.tgz && \
    tar xzvf docker-20.10.17.tgz --strip 1 -C /usr/local/bin docker/docker && \
    rm docker-20.10.17.tgz && \
    mkdir -p /root/.docker/cli-plugins && \
    curl -fsSL https://github.com/docker/buildx/releases/download/v0.9.1/buildx-v0.9.1.linux-amd64 -o /root/.docker/cli-plugins/docker-buildx && \
    chmod +x /root/.docker/cli-plugins/docker-buildx
