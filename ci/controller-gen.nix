{ buildGo125Module, lib, fetchFromGitHub }:

buildGo125Module rec {
  pname = "controller-tools";
  version = "0.20.1";
<<<<<<< HEAD
=======
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-tools/releases/download/v${version}/controller-gen-darwin-arm64";
      hash = "sha256-hJ1TRyvVyBQIXmiUKSCfQqjfwkKpG+KguQ2uxY7TYOA=";
    };
    aarch64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-tools/releases/download/v${version}/controller-gen-linux-arm64";
      hash = "sha256-tqVfGHmxvoIvQZ7+LBiV+Bf1nJJwPBU7SMyHz2ZFLT4=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-tools/releases/download/v${version}/controller-gen-linux-amd64";
      hash = "sha256-VboKH3IFy1KbA8Ca8O4dN+D/MFRLE1acC/eHOXavbvM=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;
>>>>>>> 3f781792 (nix: add support for linux-aarch64)

  src = fetchFromGitHub {
    owner = "kubernetes-sigs";
    repo = pname;
    rev = "v${version}";
    sha256 = "sha256-c1d7FlfGv7iGS+4GyhsO99OrCBIxO3M9r7jwYh7qs2o=";
  };

  vendorHash = "sha256-cFnUfcoLyFHg0JR6ix0AnpSHUGuNNVbKldKelvvMu/4=";

  ldflags = [
    "-s"
    "-w"
    "-X sigs.k8s.io/controller-tools/pkg/version.version=v${version}"
  ];

  doCheck = false;

  subPackages = [
    "cmd/controller-gen"
    "cmd/helpgen"
  ];

  meta = with lib; {
    description = "Tools to use with the Kubernetes controller-runtime libraries";
    homepage = "https://github.com/kubernetes-sigs/controller-tools";
    changelog = "https://github.com/kubernetes-sigs/controller-tools/releases/tag/v${version}";
    license = licenses.asl20;
  };
}
