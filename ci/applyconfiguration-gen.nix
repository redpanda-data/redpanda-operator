{ buildGoModule
, fetchFromGitHub
, lib
}:

buildGoModule rec {
  pname = "applyconfiguration-gen";
  version = "0.30.0";

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "kubernetes";
    repo = "code-generator";
    rev = "v${version}";
    hash = "sha256-/G9CPuvdwIBqHgkFQnXwLh8Kx1MKrZKlv/sw5ZsIBrk=";
  };

  subPackages = [ "cmd/applyconfiguration-gen" ];

  vendorHash = "sha256-SqS+v24UH/x/3zyQlWo6LDpHmlZq8YpdvJ80NN4Hrzc=";
}
