{ buildGo125Module
, fetchFromGitHub
, lib
}:

buildGo125Module rec {
  pname = "crd-ref-docs";
  version = "0.3.0";
<<<<<<< HEAD
=======
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/elastic/crd-ref-docs/releases/download/v${version}/crd-ref-docs_${version}_Darwin_arm64.tar.gz";
      hash = "sha256-ltqMJtm1A4GaXrvNJKF718qANkp1EH9pyByrRvHxY5I=";
    };
    aarch64-linux = fetchurl {
      url = "https://github.com/elastic/crd-ref-docs/releases/download/v${version}/crd-ref-docs_${version}_Linux_arm64.tar.gz";
      hash = "sha256-tZRP25JJJckFrnGGPPHIsahV5VNGx/rtWqXAIsOC+aI=";
    };
    x86_64-linux = fetchurl {
      url = "https://github.com/elastic/crd-ref-docs/releases/download/v${version}/crd-ref-docs_${version}_Linux_x86_64.tar.gz";
      hash = "sha256-HqXPxRys1Sv/OjtQ1lszzkv2E1Lw6XZiN/aJrNbRlao=";
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
    owner = "elastic";
    repo = pname;
    rev = "v${version}";
    hash = "sha256-+kg9Ql6LrvQO9UJ0lBEXJpedMBpmasTwYgRg9pj8420=";
  };

  subPackages = [
    "."
    "./config"
  ];

  vendorHash = "sha256-lV9kMqdB4hA9v/748fJH9TnE5angryLdguAEjuMhUjM=";

  meta = with lib; {
    description = "Generates Kubernetes CRD API reference documentation";
    homepage = "https://github.com/elastic/crd-ref-docs";
    license = licenses.asl20;
    mainProgram = "crd-ref-docs";
  };
}
