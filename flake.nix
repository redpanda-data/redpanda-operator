{
  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    devshell = {
      url = "github:numtide/devshell";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{ self
    , devshell
    , flake-parts
    , nixpkgs
    }: flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [ "aarch64-darwin" "x86_64-linux" "aarch64-linux" ];

      imports = [
        devshell.flakeModule
      ];

      perSystem = { self', system, ... }:
        let
          lib = pkgs.lib;
          pkgs = import nixpkgs {
            inherit system;
            overlays = [
              # Load in various overrides for custom packages and version pinning.
              (import ./ci/overlay.nix { pkgs = pkgs; })
            ];
          };
        in
        {
          formatter = pkgs.nixpkgs-fmt;

          devshells.default = {
            env = [
              { name = "GOPRIVATE"; value = "github.com/redpanda-data/flux-controller-shim"; }
            ];

            packages = [
              pkgs.diffutils # Provides `diff`, used by golangci-lint.
              pkgs.docker-client
              pkgs.gawk # GNU awk, used by some build scripts.
              pkgs.gnused # Stream Editor, used by some build scripts.
              pkgs.go-task
              pkgs.go_1_22_7
              pkgs.gofumpt
              pkgs.golangci-lint
              pkgs.k3d # Kind alternative that allows adding/removing Nodes.
              pkgs.openssl
              pkgs.setup-envtest # Kubernetes provided test utilities
              # TODO(chrisseto): Migrate taskfile to using dependencies from
              # this flake.
              # pkgs.actionlint # Github Workflow definition linter https://github.com/rhysd/actionlint
              # pkgs.goreleaser
              # pkgs.gotools
              # pkgs.kind
              # pkgs.kubectl
              # pkgs.kubernetes-helm
              # pkgs.kustomize
              # pkgs.kuttl
              # pkgs.yq
            ] ++ lib.optionals pkgs.stdenv.isLinux [
              pkgs.sysctl # Used to adjust ulimits on linux systems (Namely, CI).
            ];
          };
        };
    };
}
