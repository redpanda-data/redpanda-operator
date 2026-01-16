{ buildGoModule, lib, fetchFromGitHub }:

buildGoModule rec {
  pname = "kube-openapi";
  version = "4e65d59e963e749fa7939999518f9e90682983c3";

  src = fetchFromGitHub {
    owner = "kubernetes";
    repo = "kube-openapi";
    rev = "${version}";
    sha256 = "sha256-gB6jchb2V5HIo48M0wRpEmynoJN23S3OL8YCfkkxyF8=";
  };

  vendorHash = "sha256-ntKV2GMMt4R46opWP3rdB+Cnu/y6sRTYZR6uDSTbWLc=";

  ldflags = [
    "-s"
    "-w"
  ];

  doCheck = false;

  subPackages = [
    "cmd/openapi-gen"
  ];

  meta = with lib; {
    description = "Tools to use with the Kubernetes code generation";
    homepage = "https://github.com/kubernetes/kube-openapi";
    changelog = "https://github.com/kubernetes/kube-openapi";
    license = licenses.asl20;
  };
}
