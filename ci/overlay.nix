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
<<<<<<< HEAD
=======
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
      aarch64-linux = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.10.3-linux-arm64.tar.gz";
        hash = "sha256-MGgI4iadlggT6nI22+D1xWH6q3E5x8gtJ5M6FhnCMgE=";
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
      aarch64-linux = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.19.1-linux-arm64.tar.gz";
        hash = "sha256-LZbXTcWJ/x6WvjGxyw2xM3esE6sDPcSf1j+TqGD7b6Y=";
      };
      x86_64-linux = pkgs.fetchzip {
        url = "https://get.helm.sh/helm-v3.19.1-linux-amd64.tar.gz";
        hash = "sha256-8feTouvv+I89Lqsg732jQZBxUcwVqr/c8RuDUYpoUK4=";
      };
    };
  };
>>>>>>> 3f781792 (nix: add support for linux-aarch64)
})
