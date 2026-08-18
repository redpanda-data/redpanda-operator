{ pkgs
}:
let
  mkHelm = pkgs.callPackage ./helm.nix { };
in
(final: prev: {
  backport = pkgs.callPackage ./backport.nix { };
  bk = pkgs.callPackage ./bk.nix { };
  code-generator = pkgs.callPackage ./code-generator.nix { };
  controller-gen = pkgs.callPackage ./controller-gen.nix { };
  crd-ref-docs = pkgs.callPackage ./crd-ref-docs.nix { };
  docker-tag-list = pkgs.callPackage ./docker-tag-list.nix { };
  goverter = pkgs.callPackage ./goverter.nix { };
  kuttl = pkgs.callPackage ./kuttl.nix { };
  licenseupdater = pkgs.callPackage ./licenseupdater.nix { };
  rp-controller-gen = pkgs.callPackage ./rp-controller-gen.nix { };
  setup-envtest = pkgs.callPackage ./setup-envtest.nix { };
  vcluster = pkgs.callPackage ./vcluster.nix { };
  # nixpkgs still ships 1.26.5 as go_1_26; go.mod requires 1.26.6 and the
  # devshell sets GOTOOLCHAIN=local, so build 1.26.6 from the upstream source
  # tarball. Scoped to a new attribute (rather than overriding go_1_26) so the
  # rest of nixpkgs' go-built packages keep their binary-cached builds.
  #
  # To compute the hash for a new version:
  #   curl -sL https://go.dev/dl/go<VERSION>.src.tar.gz | openssl dgst -sha256 -binary | openssl base64 -A
  # and prepend "sha256-".
  go_1_26_6 = prev.go_1_26.overrideAttrs (oldAttrs: {
    version = "1.26.6";
    src = prev.fetchurl {
      url = "https://go.dev/dl/go1.26.6.src.tar.gz";
      hash = "sha256-oHIcVMaIkBRI13rZs+x+p8R0cwdV/4kTgukuy5P/LLE=";
    };
  });
  # The hashes below are the unpacked (NAR) hashes of the release tarballs, as
  # required by fetchzip. To compute one for a new version/platform:
  #
  #   nix hash convert --hash-algo sha256 --to sri \
  #     "$(nix-prefetch-url --unpack --type sha256 https://get.helm.sh/helm-v<VERSION>-<PLATFORM>.tar.gz)"
  #
  # Where <PLATFORM> is one of darwin-arm64 or linux-amd64.
  helm-3-10-3 = mkHelm {
    version = "3.10.3";
    versionSuffix = true;
    src = {
      aarch64-darwin = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.10.3-darwin-arm64.tar.gz";
        hash = "sha256-3W/piPZvkyrGOLCgghn7j9CgNxAVvWn1kwFb8Von9Ko=";
      };
      x86_64-linux = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.10.3-linux-amd64.tar.gz";
        hash = "sha256-XAtiT7vaSBrfrj03gbcQUmUMQSZ9+5nymxfVSOnQ+sM=";
      };
    };
  };
  helm-3-19-1 = mkHelm {
    version = "3.19.1";
    src = {
      aarch64-darwin = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.19.1-darwin-arm64.tar.gz";
        hash = "sha256-MrWws7eObrZUpP/xU1hElbSloa8GZFwI4rwENNP9ez8=";
      };
      x86_64-linux = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.19.1-linux-amd64.tar.gz";
        hash = "sha256-8feTouvv+I89Lqsg732jQZBxUcwVqr/c8RuDUYpoUK4=";
      };
    };
  };
})
