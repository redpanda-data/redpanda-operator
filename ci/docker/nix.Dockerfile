# syntax=docker/dockerfile:1
FROM nixos/nix:2.21.2

WORKDIR "/work"

# Enable the nix command and support for flakes.
RUN echo 'experimental-features = nix-command flakes' >> /etc/nix/nix.conf \
	# Fix a weird docker issues. See https://github.com/NixOS/nix/issues/5258
	&& echo 'filter-syscalls = false' >> /etc/nix/nix.conf \
	# Git behaves a bit strangely if the directory holding the .git folder is
	# owned by someone other than the running user.
	# (https://github.com/git/git/commit/8959555cee7ec045958f9b6dd62e541affb7e7d9)
	# This affects us in two ways:
	# 1. A weird interaction with nix flakes (https://github.com/NixOS/nix/issues/10202)
	# 2. Trying to run git commands from within this image for CI builds.
	# We could try to specify a subset of directories but given that the issues
	# only pop up in dockerized buildkite builds, it feels fairly safe and much
	# easier to disable this check entirely:
	&& git config --global --add safe.directory '*'

ENTRYPOINT ["nix", "develop", "--impure", "--command"]
