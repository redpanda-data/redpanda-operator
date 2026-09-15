{ buildGo125Module, lib, fetchFromGitHub }:

buildGo125Module rec {
  pname = "bk";
  version = "3.8.0";
<<<<<<< HEAD
=======
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url --unpack $URL)
    aarch64-darwin = fetchzip {
      url = "https://github.com/buildkite/cli/releases/download/v${version}/bk_${version}_macOS_arm64.zip";
      hash = "sha256-yZ0C0+ugRU9UQ0vkddWhirM8NM3JwZSEsafGh8QuwAo=";
    };
    aarch64-linux = fetchzip {
      url = "https://github.com/buildkite/cli/releases/download/v${version}/bk_${version}_linux_arm64.tar.gz";
      hash = "sha256-3N/s+1ft5UuUau88KZLd7cB57+sxC1YnlzCio3vfxRA=";
    };
    x86_64-linux = fetchzip {
      url = "https://github.com/buildkite/cli/releases/download/v${version}/bk_${version}_linux_amd64.tar.gz";
      hash = "sha256-X+MWRmlqL42oSwwRenS7Vkyvmp8ydXkqjWLeaMi3jDY=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;
>>>>>>> 3f781792 (nix: add support for linux-aarch64)

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
