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

### Alternative developer setup

Given Kubernetes cluster to speed up developer lifecycle one could use `task build:image` to
build `localhost/redpanda-operator:dev` container. Then retag operator container as follows
```bash
docker tag localhost/redpanda-operator:dev YOUR_PUBLIC_CONTAINER_REGISTRY/YOUR_CONTAINER_REPO:NEW_UNIQUE_TAG
docker push YOUR_PUBLIC_CONTAINER_REGISTRY/YOUR_CONTAINER_REPO:NEW_UNIQUE_TAG
```
Your Kubernetes cluster needs to be configured with you container registry in order to be able
to pull your fresh container (the easiest would be publicly accessible container registry).
Last step would be to perform the following command
```bash
kubectl set image deployment/OPERATOR_DEPLOYMENT_NAME manager=YOUR_CONTAINER_REGISTRY:YOUR_CONTAINER_TAG 
```

## Branching

This repository follows [trunk based development](https://trunkbaseddevelopment.com/) practices.

Releases are performed with git tags on `main` until breaking/backwards incompatible changes are necessary in which case we will ["branch late"](https://trunkbaseddevelopment.com/branch-for-release/#late-creation-of-release-branches).

The "map" of active branches can be found in the [README.md](README.md) of `main`.

## Backporting

We are currently experimenting with workflows for backporting leveraging the
[backport CLI](https://github.com/sorenlouv/backport). To do a manual backport, once a PR
has merged, ensure that you have a Github personal access token in
`~/.backport/config.json` as documented [here](https://github.com/sorenlouv/backport/blob/v9.6.6/docs/config-file-options.md#global-config-backportconfigjson),
and then run `backport --pr ###` with the PR number.

We will eventually try and set up an automated Github action for backports based on PR labels.

## CHANGELOG Management

Our CHANGELOG.mds are managed with [Changie](https://github.com/miniscruff/changie).
It's configuration is in [`.changie.yaml`](.changie.yaml) and various other files are in [`.changes/`](.changes/).

Whenever a user facing change is made, a change log entry should be added via `changie new`.

The `changie merge` command will regenerate all CHANGELOG.mds and should be run upon every commit.
(This is automatically handled by `task generate`).

### CHANGELOG GHA Check

As it's easy to accidentally forget to add changelog entries, a [GitHub Action](.github/workflows/changelog.yml)
checks that PRs contain a diff to the `.changes/unreleased` directory.

If a PR does not contain any user facing changes, the check can be disabled by
applying the "no-changelog" label.

## Releasing

### Release Management

This repository is a mono-repo with long lived release branches. The
individually releasable pieces are sometimes interconnected and we often find
ourselves pushing fixes in last minute. In order to prevent context loss and to
preserve our own sanity releases are coordinated and managed using [Jira
Releases](https://redpandadata.atlassian.net/projects/K8S?selectedItem=com.atlassian.jira.jira-projects-plugin%3Arelease-page).

### Cutting a Release

To release any project in this repository:
1. Mint the version and its CHANGELOG.md entry via `changie batch -j <project> <version>`
    - If minting a pre-release, specify `-k` to keep the unreleased changes in place for the official release.
2. Run `task test:unit` and `task lint`, they will report additional required actions, if any.
4. Commit the resultant diff with the commit message `<project>: cut release <version>` and rebase it into master via a Pull Request.
5. Tag the above commit with as `<project>/v<version>` with `git tag $(changie latest -j <project>) <commit-sha>`.
6. Push the tags.
7. Verify that the Release Workflow ran successfully.
8. If applicable, mark the newly minted release as the "latest".

## Nightly build

The step for nightly build is defined in Buildkite 
[pipeline.yaml definition](https://github.com/redpanda-data/redpanda-operator/blob/main/.buildkite/pipeline.yml#L43-L74).

### Scheduled builds

Buildkite has configured periodic jobs that build and pushes operator container with operator
helm chart to https://hub.docker.com/r/redpandadata/redpanda-operator-nightly container repository.
The branches that have configured scheduled build can be found in 
[Branches section in README.md](https://github.com/redpanda-data/redpanda-operator/blob/main/README.md#branches).

### Manual nightly build

In Buildkite anyone with the access to redpanda-operator project can trigger build using 
"New" button upper right corner.

![new button](./.github/buildkite-new-button.png) 

As the pop up show up please set specific branch with the `NIGHTLY_RELEASE=true`
environment variable see the following picture 

![buildkite pop up](./.github/buildkite-create-pipeline-pop-up.png)

This will build operator container image and operator helm chart. Those artifacts will be pushed to
https://hub.docker.com/r/redpandadata/redpanda-operator-nightly.
