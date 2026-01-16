{ buildGoModule, lib, fetchFromGitHub }:

buildGoModule rec {
  pname = "kube-openapi";
  version = "4e65d59e963e749fa7939999518f9e90682983c3";

  src = fetchFromGitHub {
    owner = "kubernetes";
    repo = "kube-openapi";
    rev = "${version}";
    sha256 = "sha256-AlDMB7rdqG/EvRuyQ8K0MELn16tyo2HNcPR4tR1XPK8=";
  };

  vendorHash = "sha256-VEymgJ6U9QrKcFm5G6uvB3qVHbuze8GrJ+zCaifWeVk=";

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
