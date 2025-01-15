{ buildGoModule
, fetchFromGitHub
, lib
}:

buildGoModule rec {
  pname = "crd-ref-docs";
  version = "0.1.0";

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "elastic";
    repo = pname;
    rev = "v${version}";
    hash = "sha256-Vp4HrT6Fj+stj2xasyz7i+UuogsFHeHOT8Ro0HeoGpI=";
  };

  subPackages = [
    "."
    "./config"
  ];

  vendorHash = "sha256-kNynNSa7xi5BL2oWBq7A2SWtjm1E+CyJd5f0FXkHAII=";

  meta = with lib; {
    description = "Generates Kubernetes CRD API reference documentation";
    homepage = "https://github.com/elastic/crd-ref-docs";
    license = licenses.asl20;
    mainProgram = "crd-ref-docs";
  };
}
