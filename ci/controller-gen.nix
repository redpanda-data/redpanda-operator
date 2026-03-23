{ stdenv
, fetchurl
, lib
}:
let
  pname = "controller-gen";
  version = "0.20.1";
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-tools/releases/download/v${version}/controller-gen-darwin-arm64";
      hash = "sha256-hJ1TRyvVyBQIXmiUKSCfQqjfwkKpG+KguQ2uxY7TYOA=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kubernetes-sigs/controller-tools/releases/download/v${version}/controller-gen-linux-amd64";
      hash = "sha256-VboKH3IFy1KbA8Ca8O4dN+D/MFRLE1acC/eHOXavbvM=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  phases = [ "installPhase" ];

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    cp $src "$out/bin/controller-gen"
    chmod 755 "$out/bin/controller-gen"
    runHook postInstall
  '';

  meta = with lib; {
    description = "Tools to use with the Kubernetes controller-runtime libraries";
    homepage = "https://github.com/kubernetes-sigs/controller-tools";
    changelog = "https://github.com/kubernetes-sigs/controller-tools/releases/tag/v${version}";
    license = licenses.asl20;
  };
}
