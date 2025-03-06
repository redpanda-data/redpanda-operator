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
          nodePkgs = (import ./ci/backport.nix { pkgs = pkgs; nodejs = pkgs."nodejs_23"; });
        in
        {
          formatter = pkgs.nixpkgs-fmt;

          devshells.default = {
            env = [
              { name = "GOPRIVATE"; value = "github.com/redpanda-data/flux-controller-shim"; }
              { name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.29.x)"; }
              { name = "PATH"; eval = "$(pwd)/.build:$PATH"; }
            ];

            # If the version of the installed binary is important make sure to
            # update TestToolVersions.
            packages = [
              nodePkgs."backport-9.6.6"
              pkgs.applyconfiguration-gen
              pkgs.buildkite-agent
              pkgs.changie # Changelog manager
              pkgs.chart-testing
              pkgs.controller-gen
              pkgs.crd-ref-docs # Generates documentation from CRD definitions. Used by our docs but present here to let us control and test the config.yaml
              pkgs.diffutils # Provides `diff`, used by golangci-lint.
              pkgs.docker-client
              pkgs.docker-tag-list
              pkgs.gawk # GNU awk, used by some build scripts.
              pkgs.gnused # Stream Editor, used by some build scripts.
              pkgs.go-licenses
              pkgs.go-task
              pkgs.go-tools
              pkgs.go_1_23
              pkgs.gofumpt
              pkgs.golangci-lint
              pkgs.gotestsum
              pkgs.helm-3-10-3
              pkgs.helm-docs
              pkgs.jq
              pkgs.k3d # Kind alternative that allows adding/removing Nodes.
              pkgs.kind
              pkgs.kubectl
              pkgs.kubernetes-helm
              pkgs.kustomize
              pkgs.kuttl
              pkgs.node2nix
              pkgs.openssl
              pkgs.setup-envtest # Kubernetes provided test utilities
              pkgs.yq-go
              # pkgs.actionlint # Github Workflow definition linter https://github.com/rhysd/actionlint
              # pkgs.gotools
            ] ++ lib.optionals pkgs.stdenv.isLinux [
              pkgs.sysctl # Used to adjust ulimits on linux systems (Namely, CI).
            ];
          };
        };
    };
}
