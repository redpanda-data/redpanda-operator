---
name: cut-operator-release
description: Use when the user asks to cut/mint/release a new operator version (e.g. "cut operator v25.3.5", "release operator 25.3.5"). Runs changie, bumps Chart.yaml, regenerates artifacts, and creates the release commit. Operator-only — for charts/redpanda or other projects, defer to CONTRIBUTING.md.
---

# Cut an operator release

Automates the operator release process from `operator/CLAUDE.md` /
`CONTRIBUTING.md`. Stops at checkpoints so you can review the generated
changelog before it lands in a commit.

## Inputs

- **New version** — required, in `vMAJOR.MINOR.PATCH` form (e.g. `v25.3.5`).
  If the user gave the version without a leading `v`, add it.

Do not guess. If the user did not state the version, ask before running
anything.

## Preconditions to verify before starting

1. The repo's working tree is on the branch the user wants the release commit
   on. Print `git status --short` and the branch name; abort if the user
   wanted a different branch.
2. There are no uncommitted changes that would get swept into the release
   commit. If there are, ask before continuing.
3. `nix develop -c changie --version` works (the rest of the steps run inside
   the nix devshell).

## Steps

Run each step with `Bash`. Set `NEW_VERSION` (with `v` prefix) and
`NEW_CHART_VERSION` (without `v` prefix) once and reuse:

```bash
NEW_VERSION=v25.3.5                 # ← from user input
NEW_CHART_VERSION=${NEW_VERSION#v}   # 25.3.5
```

Derive the current chart version from `operator/chart/Chart.yaml` (do NOT
hardcode — this checkout may not be at the same point as the example):

```bash
CURRENT_CHART_VERSION=$(awk '/^version: /{print $2; exit}' operator/chart/Chart.yaml)
```

### 1. Mint the changelog entry

```bash
nix develop -c changie batch -j operator "$NEW_VERSION"
```

This creates `.changes/operator/$NEW_VERSION.md` from accumulated
`.changes/unreleased/operator-*` entries.

### 2. **Checkpoint: review the generated changelog**

Read `.changes/operator/$NEW_VERSION.md` and show it to the user. Ask
whether the wording, grouping, and entry list look right. Common edits:
fixing capitalization, merging duplicates, dropping internal-only entries.

Do not continue until the user confirms or finishes their edits.

### 3. Bump `operator/chart/Chart.yaml`

```bash
sed -i \
  -e "s/^version: ${CURRENT_CHART_VERSION}$/version: ${NEW_CHART_VERSION}/" \
  -e "s/^appVersion: v${CURRENT_CHART_VERSION}$/appVersion: ${NEW_VERSION}/" \
  operator/chart/Chart.yaml
```

The repo's `nix develop` shell uses GNU sed, so no `''` after `-i`. Verify
the change took:

```bash
grep -E '^(version|appVersion):' operator/chart/Chart.yaml
```

If either line still shows the old version, stop and ask the user — the
file shape may have changed since this skill was written.

### 4. Regenerate code, templates, and goldens

```bash
nix develop -c task generate lint-fix
nix develop -c task generate lint-fix
```

The second run is intentional — the first pass updates inputs that the
second pass then propagates (e.g. chart templates feeding into lint-fixed
golden files). If the second run still produces a non-empty `git diff`,
run it a third time and report the diff to the user.

### 5. Regenerate lifecycle golden files

```bash
nix develop -c go test -v -run '^TestV2ResourceClient$' ./operator/internal/lifecycle/... -update
```

This rebuilds `operator/internal/lifecycle/testdata/*.golden.txtar` against
the new chart version. If it fails, surface the error verbatim — these
goldens are sensitive to image tag env vars (`TEST_REDPANDA_REPO`,
`TEST_REDPANDA_VERSION`) and the failure mode is usually environment.

### 6. **Checkpoint: review the staged diff**

```bash
git status --short
git diff --stat
```

Show the user. Confirm that the only changes are:

- `.changes/operator/$NEW_VERSION.md` (new)
- `.changes/unreleased/operator-*` (removed)
- `operator/CHANGELOG.md` (updated)
- `operator/chart/Chart.yaml` (version + appVersion)
- `operator/chart/README.md` (badge — picked up by `task generate`)
- `operator/chart/testdata/template-cases.golden.txtar`
- `operator/internal/lifecycle/testdata/*.golden.txtar`

If anything outside that set changed, stop and ask before committing —
generation can pull in unrelated files when other source has drifted.

### 7. Stage and commit

Stage only the release-related paths (avoid `git add -A`):

```bash
git add \
  .changes/ \
  operator/CHANGELOG.md \
  operator/chart/Chart.yaml \
  operator/chart/README.md \
  operator/chart/testdata/ \
  operator/internal/lifecycle/testdata/

git commit \
  -m "operator: cut release ${NEW_VERSION}" \
  -m "$(cat .changes/operator/${NEW_VERSION}.md)"
```

The two-`-m` form yields a commit with the release title as the subject
and the full changelog entry as the body, matching the operator's existing
release commit history.

### 8. Final report

Print:

- The new commit hash (`git log -1 --pretty='%h %s'`).
- A one-line reminder that pushing / tagging / cutting a PR is NOT
  performed by this skill — the user controls those.

## When NOT to use this skill

- **Non-operator releases.** charts/redpanda, charts/console, connectors,
  gotohelm — each has its own version replacements and golden files.
  Defer to `CONTRIBUTING.md` and do them manually.
- **Pre-releases that need to keep `unreleased/` entries.** That's the
  `changie batch -k` flag; this skill does not pass it.
- **The user wants the release tagged or pushed in the same step.** This
  skill stops at the commit on purpose.

## Failure modes worth surfacing

- **`changie batch` says "no unreleased changes"** — there is nothing to
  release; either the user already released this version or the
  `.changes/unreleased/` folder is empty.
- **`task generate` modifies files outside the operator tree** — usually
  means generation picked up drift in `charts/`. Pause and ask the user
  whether to include those in the operator release commit (almost
  always: no).
- **Lifecycle golden test fails with image-tag mismatches** — the test
  needs `TEST_REDPANDA_REPO` and `TEST_REDPANDA_VERSION` env vars (see
  `CLAUDE.md`). Surface the original error rather than retrying blindly.