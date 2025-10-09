{ buildGoModule, lib, fetchFromGitHub }:

buildGoModule rec {
  pname = "goverter";
  version = "1.9.1";

  src = fetchFromGitHub {
    owner = "jmattheis";
    repo = pname;
    rev = "v${version}";
    sha256 = "sha256-7uzkSI6ZqkCu+rhC2KMHU2i0geedy6gOKiLZwsFDExM=";
  };

  vendorHash = "sha256-wStuQhxrzd+LyHQi+k6ez6JT1xzZcPjJa09WqX70bys=";

  doCheck = false;

  subPackages = [
    "cmd/goverter"
  ];

  meta = with lib; {
    description = "goverter is a tool for creating type-safe converters";
    homepage = "https://goverter.jmattheis.de/";
    license = licenses.mit;
  };
}
