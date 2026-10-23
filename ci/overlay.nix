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

  # This nixpkgs pin ships 1.26.0 as go_1_26; go.mod requires 1.26.10 and the
  # devshell sets GOTOOLCHAIN=local, so build 1.26.10 from the upstream source
  # tarball. Scoped to a new attribute (rather than overriding go_1_26) so the
  # rest of nixpkgs' go-built packages keep their binary-cached builds.
  #
  # To compute the hash for a new version:
  #   curl -sL https://go.dev/dl/go<VERSION>.src.tar.gz | openssl dgst -sha256 -binary | openssl base64 -A
  # and prepend "sha256-".
  go_1_26_10 = prev.go_1_26.overrideAttrs (oldAttrs: {
    version = "1.26.10";
    src = prev.fetchurl {
      url = "https://go.dev/dl/go1.26.10.src.tar.gz";
      hash = "sha256-/WPog7ovK/ypPBZGsx6+9j4rHk0rMXyXidl2po2B828=";
    };
  });
})
