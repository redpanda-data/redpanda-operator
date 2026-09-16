{ buildGo125Module
, fetchFromGitHub
, lib
}:

buildGo125Module rec {
  pname = "setup-envtest";
  version = "0.20.4";
<<<<<<< HEAD
=======
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-runtime/releases/download/v${version}/setup-envtest-darwin-arm64";
      hash = "sha256-BfdhR1wRvvC6Doq72lh4QeMhsQ2GaDQ1YJk33qeniSI=";
    };
    aarch64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-runtime/releases/download/v${version}/setup-envtest-linux-arm64";
      hash = "sha256-DEZk+DSVCeFDYnScV2rXTlWVJq2YKzkTaursFrVPP6Q=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-runtime/releases/download/v${version}/setup-envtest-linux-amd64";
      hash = "sha256-UN+bRzmuPqBqx+B9Vx1VvyxxgNgKwUsFw2g97y42/6s=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;
>>>>>>> 3f781792 (nix: add support for linux-aarch64)

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "kubernetes-sigs";
    repo = "controller-runtime";
    rev = "v${version}";
    hash = "sha256-ejkllRd2hmcCimctg6/avxOU7oLguDG7QKexsOn3Eq8=";
  };

  sourceRoot = "source/tools/setup-envtest";

  vendorHash = "sha256-s7JQdVcTi+EFjnZgmuNUUtV+1u8D6Tw701J2v+9g2xw=";

  meta = with lib; {
    description = "A small tool that manages binaries for envtest";
    homepage = "https://github.com/kubernetes-sigs/controller-runtime/tree/main/tools/setup-envtest";
    license = licenses.asl20;
    mainProgram = "setup-envtest";
  };
}
