{ prev
, fetchFromGitHub
,
}:
let
  # v2 is an alpha with significant improvements but development on the project
  # seems largely stagnant.
  # https://github.com/google/go-licenses/releases/tag/v2.0.0-alpha.1
  version = "2.0.0-alpha.1";
in
prev.go-licenses.overrideAttrs {
  inherit version;

  src = fetchFromGitHub {
    owner = "google";
    repo = "go-licenses";
    tag = "v${version}";
    hash = "sha256-i6G3+zytNG1aJG5No+5C+n2P9mjBVVc3/jt2z4HdfzI=";
  };

  vendorHash = "sha256-Q3ZsUtcAc438OagXQjD8KGmCr96qNDNXcCoE278I/Qs=";

  # v1.6 requires some patches to build. v2.0.0 no longer does.
  patches = [ ];
}
