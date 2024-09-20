{ pkgs
}: (final: prev: {
  applyconfiguration-gen = pkgs.callPackage ./applyconfiguration-gen.nix { };
  controller-gen = pkgs.callPackage ./controller-gen.nix { };
  docker-tag-list = pkgs.callPackage ./docker-tag-list.nix { };
  kuttl = pkgs.callPackage ./kuttl.nix { };
  setup-envtest = pkgs.callPackage ./setup-envtest.nix { };

  # Pin to 1.22.7 (latest at time of writing) to support go.work files with
  # gopls.
  # Notably, we DO NOT override the go or go_1_22 packages. This is done for
  # two reasons:
  # 1. Doing so would break our ability to use nix's binary cache (3rd party
  # hosted) and require us rebuild all go packages within CI as there's no
  # local cache due to docker usage.
  # 2. It makes it very easy to see the exact version of go we use in flake.nix
  go_1_22_7 = prev.go_1_22.overrideAttrs (final: prev:
    let
      version = "1.22.7";
    in
    {
      inherit version;
      src = pkgs.fetchurl {
        url = "https://go.dev/dl/go${version}.src.tar.gz";
        hash = "sha256-ZkMth9heDPrD7f/mN9WTD8Td9XkzE/4R5KDzMwI8h58=";
      };
    });
})
