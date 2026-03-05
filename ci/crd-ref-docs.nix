{ stdenv
, fetchurl
, lib
}:
let
  pname = "crd-ref-docs";
  version = "0.3.0";
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/elastic/crd-ref-docs/releases/download/v${version}/crd-ref-docs_${version}_Darwin_arm64.tar.gz";
      hash = "sha256-ltqMJtm1A4GaXrvNJKF718qANkp1EH9pyByrRvHxY5I=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/elastic/crd-ref-docs/releases/download/v${version}/crd-ref-docs_${version}_Linux_x86_64.tar.gz";
      hash = "sha256-HqXPxRys1Sv/OjtQ1lszzkv2E1Lw6XZiN/aJrNbRlao=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  phases = [ "unpackPhase" "installPhase" ];

  unpackPhase = ''
    tar xzf $src
  '';

  installPhase = ''
    mkdir -p "$out/bin"
    cp crd-ref-docs "$out/bin/crd-ref-docs"
    chmod 755 "$out/bin/crd-ref-docs"
  '';

  meta = with lib; {
    description = "Generates Kubernetes CRD API reference documentation";
    homepage = "https://github.com/elastic/crd-ref-docs";
    license = licenses.asl20;
    mainProgram = "crd-ref-docs";
  };
}
