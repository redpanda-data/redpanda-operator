{ lib
, buildGo126Module
, fetchFromGitHub
,
}:

buildGo126Module rec {
  pname = "rp-controller-gen";
  version = "4f2e6f1536ddb6b168efa94f7a6b9a8014eeb561";

  src = fetchFromGitHub {
    owner = "redpanda-data";
    repo = "common-go";
    rev = "${version}";
    hash = "sha256-oJbmlj01tlC42iS1hSvwDXlDDt0DW2lQ/I6KPSgrIbA=";
  };

  vendorHash = "sha256-+JBlj0NCw3hVjJwor+VSLIoGT8YP8D7ph8xweIPzcno=";

  sourceRoot = "source/rp-controller-gen";

  subPackages = [
    "."
  ];

  doCheck = false;

  meta = {
    description = "Re-usable Redpanda controller generators";
    mainProgram = "rp-controller-gen";
    homepage = "https://github.com/redpanda-data/common-go/rp-controller-gen";
    license = with lib.licenses; [ asl20 ];
  };
}
