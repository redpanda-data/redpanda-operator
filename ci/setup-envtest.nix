{ buildGoModule
, fetchFromGitHub
, lib
}:

buildGoModule rec {
  pname = "setup-envtest";
  version = "0.20.4";

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "kubernetes-sigs";
    repo = "controller-runtime";
    rev = "v${version}";
    hash = "sha256-ejkllRd2hmcCimctg6/avxOU7oLguDG7QKexsOn3Eq8=";
  };

  sourceRoot = "source/tools/setup-envtest";

  vendorHash = "sha256-s7JQdVcTi+EFjnZgmuNUUtV+1u8D6Tw701J2v+9g2xw=";

  meta = with lib; {
    description = "A small tool that manages binaries for envtest";
    homepage = "https://github.com/kubernetes-sigs/controller-runtime/tree/main/tools/setup-envtest";
    license = licenses.asl20;
    mainProgram = "setup-envtest";
  };
}
