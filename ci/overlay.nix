{ pkgs
}: (final: prev: {
  applyconfiguration-gen = pkgs.callPackage ./applyconfiguration-gen.nix { };
  controller-gen = pkgs.callPackage ./controller-gen.nix { };
  docker-tag-list = pkgs.callPackage ./docker-tag-list.nix { };
  kuttl = pkgs.callPackage ./kuttl.nix { };
  setup-envtest = pkgs.callPackage ./setup-envtest.nix { };
  helm-3-10-3 = pkgs.callPackage ./helm.nix { };
  chart-testing = pkgs.callPackage ./chart-testing.nix { };
})
