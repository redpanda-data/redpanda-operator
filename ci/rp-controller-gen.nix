{ lib
, buildGo126Module
, fetchFromGitHub
,
}:

buildGo126Module rec {
  pname = "rp-controller-gen";
  version = "59451d668eb28f01f91354a2463d766866148ef4";

  src = fetchFromGitHub {
    owner = "redpanda-data";
    repo = "common-go";
    rev = "${version}";
    hash = "sha256-34oDV8HFbTd5w34Kf1bVVuRIo3nXnVJlkQnbwh9A5GQ=";
  };

  vendorHash = "sha256-PIKAvpLy0tTYkkzxg1UvHhDMhQGysPQ06k1J+5llN84=";

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
