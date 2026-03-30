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
              { name = "CGO_ENABLED"; value = "0"; }
              { name = "GOROOT"; value = "${pkgs.go_1_26}/share/go"; }
              { name = "KUBEBUILDER_ASSETS"; eval = "$(setup-envtest use -p path 1.32.x)"; }
              { name = "PATH"; eval = "$(pwd)/.build:$PATH"; }
              { name = "TEST_CERTMANAGER_VERSION"; eval = "v1.14.2"; }
              { name = "TEST_REDPANDA_REPO"; eval = "redpandadata/redpanda-unstable"; }
              { name = "TEST_REDPANDA_VERSION"; eval = "v26.1.1-rc1"; }
              { name = "CGO_ENABLED"; eval = "0"; }
              # K3S_IMAGE controls the Kubernetes version used by k3d-based tests.
              # Do NOT set K3S_IMAGE here — per-PR tests must use the Go default
              # from pkg/k3d/k3d.go (minimum supported version: v1.32.13-k3s1).
              # Nightly tests override to max version via Buildkite schedule env:
              #   K3S_IMAGE=rancher/k3s:v1.35.2-k3s1
              # For local forward-compat testing, export K3S_IMAGE in your shell.
              # This is a workaround for rpk packages using buf-built grpc-gateway protobuf options, whereas the kubernetes ecosystem
              # uses the google provided libraries, which conflict due to them being the same library. We put this here primarily for
              # tests run on a local environment so that we can use the same typical "go test" workflow we otherwise would normally.
              # Eventually we should remove this and our reliance on internal rpk libraries.
              { name = "GOLANG_PROTOBUF_REGISTRATION_CONFLICT"; eval = "ignore"; }
            ];

            # If the version of the installed binary is important make sure to
            # update TestToolVersions.
            packages = [
              pkgs.actionlint # Github Workflow definition linter https://github.com/rhysd/actionlint
              pkgs.awscli2
              pkgs.backport
              pkgs.bk
              pkgs.buildkite-agent
              pkgs.buf
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
              pkgs.go_1_26
              pkgs.gofumpt
              pkgs.golangci-lint
              pkgs.gotestsum
              pkgs.goverter
              pkgs.grpc-tools
              pkgs.helm-3-10-3
              pkgs.helm-docs
              pkgs.jq
              pkgs.k3d # Kind alternative that allows adding/removing Nodes.
              pkgs.kind
              pkgs.kubectl
              pkgs.kubernetes-helm
              pkgs.kustomize
              pkgs.kuttl
              pkgs.licenseupdater
              pkgs.openssl
              pkgs.otel-desktop-viewer
              pkgs.protoc-gen-go
              pkgs.rp-controller-gen
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
