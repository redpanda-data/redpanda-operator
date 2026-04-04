{ pkgs
, stdenv
, fetchurl
}:
let
  pname = "kuttl";
  version = "0.19.0";
  src = {
    aarch64-darwin = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_darwin_arm64";
      hash = "sha256-OTEJUY2OhnrLf61lGPzEqbsA15Am992bCJfrtNOeDMg=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_linux_x86_64";
      hash = "sha256-N2Fi6tKhaBUFw6RJw/AvOy5sj2BJY8WnKc975TKxrik=";
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
