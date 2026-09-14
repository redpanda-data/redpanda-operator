{ lib
, buildGo126Module
, fetchFromGitHub
,
}:

buildGo126Module rec {
  pname = "rp-controller-gen";
  version = "4653452d018ddee721a26cd9582c9c71e343bb2f";

  src = fetchFromGitHub {
    owner = "redpanda-data";
    repo = "common-go";
    rev = "${version}";
    hash = "sha256-0op+EKzSlxi8vKqH3Br2NvN5W1jzAJPaDLkmgoikny0=";
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
