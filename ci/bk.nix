{ buildGoModule, lib, fetchFromGitHub }:

buildGoModule rec {
  pname = "bk";
  version = "3.8.0";

  src = fetchFromGitHub {
    owner = "buildkite";
    repo = "cli";
    rev = "v${version}";
    sha256 = "sha256-zRACKFs4AZkWg2OqFIyiLuM1V6GyIPmtyxSOjfHkL6U=";
  };

  cgo_enabled = false;
  
  ldflags = [
    "-s"
    "-w"
    "-X github.com/buildkite/cli/v3/internal/version.Version=v${version}"
  ];

  vendorHash = "sha256-IOZd5XIUPhU52fcBYFo9+88XZcJon2RKVHnjDbOhPJ0=";

  doCheck = false;

  subPackages = [
    "cmd/bk"
  ];

  meta = with lib; {
    description = "Buildkite CLI";
    homepage = "https://github.com/buildkite/cli";
    changelog = "https://github.com/buildkite/cli/releases/tag/v${version}";
    license = licenses.mit;
  };
}
