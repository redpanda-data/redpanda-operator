{ lib
, stdenv
, buildGo124Module
, fetchFromGitHub
, go
, installShellFiles
, makeWrapper
,
}:

# This is a copy of the upstream nix package for go-licenses, modified to use
# buildGo124Module since we updated to Go 1.25 in our environment.
buildGo124Module rec {
  pname = "go-licenses";
  version = "2.0.0-alpha.1";

  src = fetchFromGitHub {
    owner = "google";
    repo = "go-licenses";
    tag = "v${version}";
    hash = "sha256-i6G3+zytNG1aJG5No+5C+n2P9mjBVVc3/jt2z4HdfzI=";
  };

  vendorHash = "sha256-Q3ZsUtcAc438OagXQjD8KGmCr96qNDNXcCoE278I/Qs=";

  nativeBuildInputs = [
    installShellFiles
    makeWrapper
  ];

  subPackages = [ "." ];

  allowGoReference = true;

  postInstall = lib.optionalString (stdenv.buildPlatform.canExecute stdenv.hostPlatform) ''
    installShellCompletion --cmd go-licenses \
      --bash <("$out/bin/go-licenses" completion bash) \
      --fish <("$out/bin/go-licenses" completion fish) \
      --zsh  <("$out/bin/go-licenses" completion zsh)

    # workaround empty output when GOROOT differs from built environment
    # see https://github.com/google/go-licenses/issues/149
    wrapProgram "$out/bin/go-licenses" \
      --set GOROOT '${go}/share/go'
  '';

  doCheck = false;

  meta = {
    changelog = "https://github.com/google/go-licenses/releases/tag/v${version}";
    description = "Reports on the licenses used by a Go package and its dependencies";
    mainProgram = "go-licenses";
    homepage = "https://github.com/google/go-licenses";
    license = with lib.licenses; [ asl20 ];
    maintainers = with lib.maintainers; [ Luflosi ];
  };
}
