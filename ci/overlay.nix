{ pkgs
}: (final: prev: {
  applyconfiguration-gen = pkgs.callPackage ./applyconfiguration-gen.nix { };
  backport = pkgs.callPackage ./backport.nix { };
  controller-gen = pkgs.callPackage ./controller-gen.nix { };
  crd-ref-docs = pkgs.callPackage ./crd-ref-docs.nix { };
  docker-tag-list = pkgs.callPackage ./docker-tag-list.nix { };
  go-licenses = pkgs.callPackage ./go-licenses.nix { inherit prev; };
  helm-3-10-3 = pkgs.callPackage ./helm.nix { };
  kuttl = pkgs.callPackage ./kuttl.nix { };
  setup-envtest = pkgs.callPackage ./setup-envtest.nix { };
  vcluster = pkgs.callPackage ./vcluster.nix { };
  bk = pkgs.callPackage ./bk.nix { };
})
