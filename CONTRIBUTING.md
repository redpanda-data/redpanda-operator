# Contributing

## Development Environment

The development environment is managed by [`nix`](https://nixos.org). If
installing nix is not an option, you can ensure all the tools listed in
[`flake.nix`](./flake.nix) are installed and available in `$PATH`. The exact
versions can been seen in [this test case](./pkg/lint/testdata/tool-versions.txtar).

To install nix, either follow the [official guide](https://nixos.org/download) or [zero-to-nix's quick start](https://zero-to-nix.com/start).

Next, you'll want to enable the experimental features `flakes` and
`nix-command` to avoid typing `--extra-experimental-features nix-command
--extra-experimental-features flakes` all the time.

```bash
# Or open the file in an editor and paste in this line.
# If you're using nix to manage your nix install, you'll have to find your own path :)
echo 'experimental-features = nix-command flakes' >> /etc/nix/nix.conf
```

Now you're ready to go!

```sh
nix develop # Enter a development shell.
nix develop -c fish  # Enter a development shell using fish
nix develop -c zsh  # Enter a development shell using zsh
```

It is recommend to use [direnv](https://direnv.net/) to automatically enter the
development shell when `cd`'ing into the repository. The [.envrc](./.envrc) is
already configured.

## CHANGELOG Management

Our CHANGELOG.mds are managed with [Changie](https://github.com/miniscruff/changie).
It's configuration is in [`.changie.yaml`](.changie.yaml) and various other files are in [`.changes/`](.changes/).

Whenever a user facing change is made, a change log entry should be added via `changie new`.

The `changie merge` command will regenerate all CHANGELOG.mds and should be run upon every commit.
(This is automatically handled by `task generate`).

## Releasing

To release any project in this repository:
1. Mint the version and its CHANGELOG.md entry via `changie batch -j <project> <version>`
2. Run `task test:unit` and `task lint`, they will report additional required actions, if any.
4. Commit the resultant diff with the commit message `<project>: cut release <version>` and rebase it into master via a Pull Request.
5. Tag the above commit with as `<project>/v<version>` with `git tag $(changie latest -j <project>) <commit-sha>`.
    - If the operator is being released, also tag the same commit as `v<version>`.
6. Push the tag(s).
