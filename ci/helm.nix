{ pkgs
, stdenv
}:
{ version
, src
  # When true, the binary is installed as `helm-${version}` instead of `helm` so
  # that multiple versions may coexist on $PATH.
, versionSuffix ? false
}:
let
  pname = "helm";
  binary = if versionSuffix then "helm-${version}" else "helm";
in
stdenv.mkDerivation {
  inherit pname version;

  src = src.${stdenv.system} or (throw "${pname}-${version}: ${stdenv.system} is unsupported.");

  installPhase = ''
    mkdir -p "$out/bin"
    cp "$src/helm" "$out/bin/${binary}"
    chmod 755 "$out/bin/${binary}"
  '';
}
