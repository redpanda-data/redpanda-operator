# syntax=docker/dockerfile:1
FROM nixos/nix:2.21.2

WORKDIR "/work"

# Enable the nix command and support for flakes.
RUN echo 'experimental-features = nix-command flakes' >> /etc/nix/nix.conf \
	# Fix a weird docker issues. See https://github.com/NixOS/nix/issues/5258
	&& echo 'filter-syscalls = false' >> /etc/nix/nix.conf \
	# Fix a weird git/flake issue. See https://github.com/NixOS/nix/issues/10202
	&& git config --global --add safe.directory .

ENTRYPOINT ["nix", "develop", "--impure", "--command"]
