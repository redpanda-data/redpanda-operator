{ buildGo126Module
, fetchFromGitHub
}:
let
  version = "2.0.1";
in
buildGo126Module {
  pname = "go-licenses";
  inherit version;

  src = fetchFromGitHub {
    owner = "google";
    repo = "go-licenses";
    tag = "v${version}";
    hash = "sha256-byKuUf8XMyXjAHZUANaBVAc6c2Jz9mEEdRxAy69P2QM=";
  };

  vendorHash = "sha256-AYbx/DmYnbjJ2iqx34t/dUsthTjJ+YDvfxxCl/cJenI=";

  doCheck = false;
}
