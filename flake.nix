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
          pkgs = import nixpkgs { inherit system; };
          lib = pkgs.lib;
        in
        {
          formatter = pkgs.nixpkgs-fmt;

          devshells.default = {
            env = [
              { name = "GOPRIVATE"; value = "github.com/redpanda-data/flux-controller-shim"; }
            ];

            packages = [
              pkgs.diffutils # Provided `diff`, used by golangci-lint.
              pkgs.docker-client
              pkgs.gawk # GNU awk, used by some build scripts.
              pkgs.gnused # Stream Editor, used by some build scripts.
              pkgs.go-task
              pkgs.go_1_21
              pkgs.openssl
              # TODO(chrisseto): Migrate taskfile to using dependencies from
              # this flake.
              # pkgs.goreleaser
              # pkgs.actionlint # Github Workflow definition linter https://github.com/rhysd/actionlint
              # pkgs.gofumpt
              # pkgs.golangci-lint
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
