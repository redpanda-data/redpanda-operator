{ stdenv
, fetchurl
, installShellFiles
,
}:
let
  pname = "vcluster";
  version = "0.23.0";
  src = {
    # Update hashes with: nix hash to-sri --type sha256 (nix-prefetch-url $URL)
    aarch64-darwin = fetchurl {
      url = "https://github.com/loft-sh/vcluster/releases/download/v${version}/vcluster-darwin-arm64";
      hash = "sha256-3NWvQramcxvZaewc3hMvYGhSOIIsnWD3mqU/WNA723c=";
    };
    x86_64-linux = fetchurl {
      url = " https://github.com/loft-sh/vcluster/releases/download/v${version}/vcluster-linux-amd64";
      hash = "sha256-4b/UKWj/1hef0A2tKlluiWVhi94FQL1qAmmTc5Q0DMc=";
    };
  }.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");
in
stdenv.mkDerivation {
  inherit pname version src;

  phases = [ "installPhase" ];
  nativeBuildInputs = [ installShellFiles ];

  installPhase = ''
    runHook preInstall
    mkdir -p "$out/bin"
    cp $src "$out/bin/vcluster"
    chmod 755 "$out/bin/vcluster"
    runHook postInstall
  '';

  # vcluster is a bit fussy because it tries to make default config directories
  # way earlier than it should and nix runs it in a RO namespace.
  # Making $HOME a writable dir and setting --config works around the problem and
  # let's us generate completions.
  postInstall = ''
    installShellCompletion --cmd vcluster \
      --bash <(HOME=$(pwd) $out/bin/vcluster --config /dev/null completion bash) \
      --fish <(HOME=$(pwd) $out/bin/vcluster --config /dev/null completion fish) \
      --zsh <(HOME=$(pwd) $out/bin/vcluster --config /dev/null completion zsh)
  '';
}
