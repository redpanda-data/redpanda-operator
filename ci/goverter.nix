{ buildGoModule, lib, fetchFromGitHub }:

buildGoModule rec {
  pname = "goverter";
  version = "1.9.2";

  src = fetchFromGitHub {
    owner = "jmattheis";
    repo = pname;
    tag = "v${version}";
    hash = "sha256-OJMZovYTPOW+jeml3J6+V7qiyNVZPxl1eHYJ1fEhdHk=";
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
