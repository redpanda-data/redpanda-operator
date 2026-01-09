{ lib
, buildGoModule
, fetchFromGitHub
,
}:

buildGoModule rec {
  pname = "rp-controller-gen";
  version = "f50515c8d09311681834eaeed1a9a9ffe6ec1397";

  src = fetchFromGitHub {
    owner = "redpanda-data";
    repo = "common-go";
    rev = "${version}";
    hash = "sha256-adNxpa+XoF+dC5MMbu1X83XdMRwLPpxF2ZQ0ofvAO44=";
  };

  vendorHash = "sha256-Xn0nFCW1YmlQhB0aKU8TSFphBLcWs7ns+ircgJbnSl4=";

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
