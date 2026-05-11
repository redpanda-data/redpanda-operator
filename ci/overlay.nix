{ pkgs
}: (final: prev: {
  backport = pkgs.callPackage ./backport.nix { };
  bk = pkgs.callPackage ./bk.nix { };
  code-generator = pkgs.callPackage ./code-generator.nix { };
  controller-gen = pkgs.callPackage ./controller-gen.nix { };
  crd-ref-docs = pkgs.callPackage ./crd-ref-docs.nix { };
  docker-tag-list = pkgs.callPackage ./docker-tag-list.nix { };
  go-licenses = pkgs.callPackage ./go-licenses.nix { };
  goverter = pkgs.callPackage ./goverter.nix { };
  helm-3-10-3 = pkgs.callPackage ./helm.nix { };
  kuttl = pkgs.callPackage ./kuttl.nix { };
  setup-envtest = pkgs.callPackage ./setup-envtest.nix { };
  vcluster = pkgs.callPackage ./vcluster.nix { };

  # Pin Go 1.25.10 (stdlib fix for CVE-2026-33811 / CVE-2026-33814 / CVE-2026-39836).
  # nixpkgs-unstable currently provides 1.25.7; overriding source so we don't have
  # to bump nixpkgs (which would pull in unrelated changes).
  go_1_25 = prev.go_1_25.overrideAttrs (oldAttrs: {
    version = "1.25.10";
    src = prev.fetchurl {
      url = "https://go.dev/dl/go1.25.10.src.tar.gz";
      hash = "sha256-IM8EqS5a+ZdI40G8iZb6KAkMmsmHZfoRXsXd9B169B0=";
    };
  });
})
