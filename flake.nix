{
  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    devshell = {
      url = "github:numtide/devshell";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    otel-tui.url = "github:ymtdzzz/otel-tui";
  };

  outputs =
    inputs@{ self
    , devshell
    , flake-parts
    , nixpkgs
    , otel-tui
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

          # Make it possible to reference the devshell context from standard
          # nix commands. e.g. nix copy .#devshell.
          packages.devshell = self'.devShells.default;

          devshells.default = {
            env = [
              { name = "GOROOT"; value = "${pkgs.go}/share/go"; }
              { name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.32.x)"; }
              { name = "PATH"; eval = "$(pwd)/.build:$PATH"; }
              { name = "TEST_CERTMANAGER_VERSION"; eval = "v1.14.2"; }
              { name = "TEST_REDPANDA_REPO"; eval = "redpandadata/redpanda-unstable"; }
              { name = "TEST_REDPANDA_VERSION"; eval = "v25.3.1-rc2"; }
              { name = "CGO_ENABLED"; eval = "0"; }
            ];

            # If the version of the installed binary is important make sure to
            # update TestToolVersions.
            packages = [
              pkgs.actionlint # Github Workflow definition linter https://github.com/rhysd/actionlint
              pkgs.awscli2
              pkgs.backport
              pkgs.bk
              pkgs.buildkite-agent
              pkgs.changie # Changelog manager
              pkgs.code-generator
              pkgs.controller-gen
              pkgs.crd-ref-docs # Generates documentation from CRD definitions. Used by our docs but present here to let us control and test the config.yaml
              pkgs.diffutils # Provides `diff`, used by golangci-lint.
              pkgs.docker-client
              pkgs.docker-tag-list
              pkgs.gawk # GNU awk, used by some build scripts.
              pkgs.gh
              pkgs.gnused # Stream Editor, used by some build scripts.
              pkgs.go-licenses
              pkgs.go-task
              pkgs.go-tools
              pkgs.go
              pkgs.gofumpt
              pkgs.golangci-lint
              pkgs.gotestsum
              pkgs.goverter
              pkgs.helm-3-10-3
              pkgs.helm-docs
              pkgs.jq
              pkgs.k3d # Kind alternative that allows adding/removing Nodes.
              pkgs.kind
              pkgs.kubectl
              pkgs.kubernetes-helm
              pkgs.kustomize
              pkgs.kuttl
              pkgs.openssl
              pkgs.otel-desktop-viewer
              pkgs.setup-envtest # Kubernetes provided test utilities
              pkgs.vcluster
              pkgs.yq-go
              otel-tui.defaultPackage.${system}
            ] ++ lib.optionals pkgs.stdenv.isLinux [
              pkgs.sysctl # Used to adjust ulimits on linux systems (Namely, CI).
            ];
          };
        };
    };
}
