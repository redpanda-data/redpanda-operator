{ buildNpmPackage
, fetchFromGitHub
, lib
, pkgs
}:

buildNpmPackage rec {
  pname = "backport";
  version = "9.6.6";

  src = fetchFromGitHub {
    owner = "sorenlouv";
    repo = "backport";
    rev = "v${version}";
    hash = "sha256-VgEOUqbsgZ0EP9dN9iRmh+V05gEUaNhKASivt0pUKIw=";
  };

  dontNpmBuild = true;

  # the compiled typescript files don't come in the release tags and neither does a package-lock.json
  # due to this project using yarn, so just copy over the checked in package-lock.json and generate
  # the typescript files prior to installation so the binary can be run. 
  preInstall = ''
    npx tsc
  '';
  packageLock = pkgs.writeText "package-lock.json" (builtins.readFile ./files/backport-package-lock.json);
  postPatch = ''
    cp ${packageLock} package-lock.json
  '';

  npmDepsHash = "sha256-ZjmP/kCDEYHHJLFyITIPlM93TFMYDSLbrRS9MGHAEvE=";

  meta = with lib; {
    description = "Backport CLI tool";
    mainProgram = "backport";
    homepage = "https://github.com/sorenlouv/backport";
    changelog = "https://github.com/sorenlouv/backport/releases/tag/v${version}";
    license = licenses.asl20;
  };
}
