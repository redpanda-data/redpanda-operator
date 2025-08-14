{ buildGoModule
, fetchFromGitHub
}:

buildGoModule rec {
  pname = "code-generator";
  version = "0.33.0";

  # Don't run tests.
  doCheck = false;
  doInstallCheck = false;

  src = fetchFromGitHub {
    owner = "kubernetes";
    repo = "code-generator";
    rev = "v${version}";
    hash = "sha256-orYME5Uzf1U/PGfYM9RzYzvy8XlEcnp1vTJXxZ1PJkA=";
  };

  subPackages = [
    "cmd/applyconfiguration-gen"
    "cmd/conversion-gen"
    "cmd/defaulter-gen"
    "cmd/register-gen"
  ];

  vendorHash = "sha256-X3OuYF9mqbH6slH6bj1zJY40hDQ5omrDC16ZiM8l5I4=";
}
