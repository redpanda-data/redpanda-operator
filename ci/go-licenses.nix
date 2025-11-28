{ prev
, fetchFromGitHub
,
}:
let
  # v2 is an alpha with significant improvements but development on the project
  # seems largely stagnant.
  # https://github.com/google/go-licenses/releases/tag/v2.0.1
  version = "2.0.1";
in
prev.go-licenses.overrideAttrs {
  inherit version;

  src = fetchFromGitHub {
    owner = "google";
    repo = "go-licenses";
    tag = "v${version}";
    hash = "sha256-byKuUf8XMyXjAHZUANaBVAc6c2Jz9mEEdRxAy69P2QM=";
  };

  vendorHash = "sha256-AYbx/DmYnbjJ2iqx34t/dUsthTjJ+YDvfxxCl/cJenI=";

  # v1.6 requires some patches to build. v2.0.0 no longer does.
  patches = [ ];
}
