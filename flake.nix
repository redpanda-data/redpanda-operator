{
  inputs = {
    nixpkgs.url = "nixpkgs";
    nixpkgs-unstable.url = "nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs =
    inputs@{ self
    , nixpkgs
    , nixpkgs-unstable
    , flake-parts
    ,
    }: flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [ "aarch64-darwin" "x86_64-linux" ];

      perSystem = { self', system, ... }:
        let
          pkgs = import nixpkgs { inherit system; };
          unstable = import nixpkgs-unstable { inherit system; };
        in
        {

          formatter = pkgs.nixpkgs-fmt;

          # GOPRIVATE = "github.com/redpanda-data/flux-controller-shim";

          devShells.default = pkgs.mkShell {
            buildInputs = [
              pkgs.go
              pkgs.gotools
              unstable.go-task
              unstable.kustomize
              unstable.golangci-lint
              unstable.gofumpt
              unstable.kubernetes-controller-tools
            ];
          };
        };
    };
}
