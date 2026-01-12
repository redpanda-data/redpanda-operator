{ lib
, buildGo124Module
, fetchFromGitHub
,
}:

buildGo124Module rec {
  pname = "licenseupdater";
  version = "315804cc1398cbcf7a1a689e3647d82e8a635b39";

  src = fetchFromGitHub {
    owner = "redpanda-data";
    repo = "common-go";
    rev = "${version}";
    hash = "sha256-YCVcRtiFW2fQwvusHVpY8MUOTP1SKP7nAEoJqGIIlmQ=";
  };

  vendorHash = "sha256-dl5zTsuggCGAWgIIMOYX2UReXscKy90WxGc/yWPv4KE";

  sourceRoot = "source/licenseupdater";

  subPackages = [
    "."
  ];

  doCheck = false;

  meta = {
    description = "Automatically keep licenses up-to-date for a repo";
    mainProgram = "licenseupdater";
    homepage = "https://github.com/redpanda-data/common-go/licenseupdater";
    license = with lib.licenses; [ asl20 ];
  };
}
