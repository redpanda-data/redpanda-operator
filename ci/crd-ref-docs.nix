{ buildGo125Module
, fetchFromGitHub
, lib
}:

buildGo125Module rec {
  pname = "crd-ref-docs";
  version = "0.3.0";

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
