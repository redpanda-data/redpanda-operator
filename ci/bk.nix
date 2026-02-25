{ stdenv
, fetchzip
, lib
}:
let
  pname = "bk";
  version = "3.8.0";
  src = {
    # Update hashes with: nix hash to-sri --type sha256 $(nix-prefetch-url --unpack $URL)
    aarch64-darwin = fetchzip {
      url = "https://github.com/buildkite/cli/releases/download/v${version}/bk_${version}_macOS_arm64.zip";
      hash = "sha256-yZ0C0+ugRU9UQ0vkddWhirM8NM3JwZSEsafGh8QuwAo=";
    };
    x86_64-linux = fetchzip {
      url = "https://github.com/buildkite/cli/releases/download/v${version}/bk_${version}_linux_amd64.tar.gz";
      hash = "sha256-X+MWRmlqL42oSwwRenS7Vkyvmp8ydXkqjWLeaMi3jDY=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  installPhase = ''
    mkdir -p "$out/bin"
    cp "$src/bk" "$out/bin/bk"
    chmod 755 "$out/bin/bk"
  '';

  meta = with lib; {
    description = "Buildkite CLI";
    homepage = "https://github.com/buildkite/cli";
    changelog = "https://github.com/buildkite/cli/releases/tag/v${version}";
    license = licenses.mit;
  };
}
