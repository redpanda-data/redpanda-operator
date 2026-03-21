{ pkgs
, stdenv
, fetchurl
}:
let
  pname = "kuttl";
  version = "0.25.0";
  src = {
    aarch64-darwin = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_darwin_arm64";
      hash = "sha256-hkuVA3QD54B3eSnx5C/rwACozQWEVZePmGodzbNZpTY=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_linux_x86_64";
      hash = "sha256-qiMK5qFMQp74ivkFQ+KbOf5b783KIBwCkbt4pNjj+NA=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  phases = [ "installPhase" ];

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    cp $src "$out/bin/kuttl"
    chmod 755 "$out/bin/kuttl"
    runHook postInstall
  '';
}
