{ buildGoModule
, fetchFromGitHub
, lib
}:

buildGoModule rec {
  pname = "crd-ref-docs";
  version = "0.2.0";

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "elastic";
    repo = pname;
    rev = "v${version}";
    hash = "sha256-JKaph5QDIWFDNEI8k9ZKYxZyRRAaQLogLpZKVwoU0K0=";
  };

  subPackages = [
    "."
    "./config"
  ];

  vendorHash = "sha256-DJnhoqbOZ+808In5I45vpKoVgo4Comog+X2gAuQTirA=";

  meta = with lib; {
    description = "Generates Kubernetes CRD API reference documentation";
    homepage = "https://github.com/elastic/crd-ref-docs";
    license = licenses.asl20;
    mainProgram = "crd-ref-docs";
  };
}
