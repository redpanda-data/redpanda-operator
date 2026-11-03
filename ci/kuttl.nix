{ pkgs
, stdenv
, fetchurl
}:
let
  pname = "kuttl";
  version = "0.28.0";
  src = {
    aarch64-darwin = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_darwin_arm64";
      hash = "sha256-x93Td1N+lOBbbTDhB14uuww4Nt8vZ2PLNyoyJ7l4ihQ=";
    };
    aarch64-linux = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_linux_arm64";
      hash = "sha256-qloik2eO04eZNGUFWnYW0Q3TxRSzRWz1m4Uwf6+4niY=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/kudobuilder/kuttl/releases/download/v${version}/kubectl-kuttl_${version}_linux_x86_64";
      hash = "sha256-OYqk2C0Xsnehinp2DhL4KX1469u/0ifee/4Cnc/Kbxw=";
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
