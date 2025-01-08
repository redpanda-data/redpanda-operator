# Contributing

## CHANGELOG Management

Our CHANGELOG.mds are managed with [Changie](https://github.com/miniscruff/changie).
It's configuration is in [`.changie.yaml`](.changie.yaml) and various other files are in [`.changes/`](.changes/).

Whenever a user facing change is made, a change log entry should be added via `changie new`.

The `changie merge` command will regenerate all CHANGELOG.mds and should be run upon every commit.
(This is automatically handled by `task generate`).

## Releasing

To release any project in this repository:
1. Mint the version and its CHANGELOG.md entry via `changie batch -j <project> <version>`
2. Commit the resultant diff with the commit message `<project>: cut release <version>` and rebase it into master via a Pull Request.
3. Tag the above commit with as `<project>-v<version>` with `git tag $(changie latest -j <project>) <commit-sha>`.
    - If the operator is being released, also tag the same commit as `v<version>`.
5. Push the tag(s).
