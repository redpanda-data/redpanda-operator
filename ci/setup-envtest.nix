{ stdenv
, fetchurl
, lib
}:
let
  pname = "setup-envtest";
  version = "0.20.4";
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-runtime/releases/download/v${version}/setup-envtest-darwin-arm64";
      hash = "sha256-BfdhR1wRvvC6Doq72lh4QeMhsQ2GaDQ1YJk33qeniSI=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-runtime/releases/download/v${version}/setup-envtest-linux-amd64";
      hash = "sha256-UN+bRzmuPqBqx+B9Vx1VvyxxgNgKwUsFw2g97y42/6s=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  phases = [ "installPhase" ];

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    cp $src "$out/bin/setup-envtest"
    chmod 755 "$out/bin/setup-envtest"
    runHook postInstall
  '';

  meta = with lib; {
    description = "A small tool that manages binaries for envtest";
    homepage = "https://github.com/kubernetes-sigs/controller-runtime/tree/main/tools/setup-envtest";
    license = licenses.asl20;
    mainProgram = "setup-envtest";
  };
}
