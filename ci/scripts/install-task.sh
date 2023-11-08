#!/bin/bash
set -eu

TASK_VERSION="3.31.0"

if [[ $(uname -m) == "aarch64" ]]; then
  ARCH="arm64"
else
  ARCH="amd64"
fi

mkdir -p bin/

curl -sSLf https://vectorized-public.s3.us-west-2.amazonaws.com/dependencies/task_linux_${TASK_VERSION}_${ARCH}.tar.gz | tar -xz -C bin/ task

