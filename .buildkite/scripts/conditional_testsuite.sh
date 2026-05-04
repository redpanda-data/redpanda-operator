#!/bin/bash
set -eo pipefail

testsuite=".buildkite/testsuite.yml"

if [[ "$BUILDKITE_SOURCE" == "webhook" && -n "$BUILDKITE_PULL_REQUEST" && "$BUILDKITE_PULL_REQUEST" != "false" ]]; then
  # It's a PR, let's check what changed
  base_branch="$BUILDKITE_PULL_REQUEST_BASE_BRANCH"
  if [[ -z "$base_branch" ]]; then
    base_branch="main"
  fi

  # Fetch base branch to compare against
  git fetch origin -- "$base_branch"

  # Check if any non-skippable files changed. Skippable paths are docs,
  # GitHub workflows, and root-level Markdown files (README, CONTRIBUTING,
  # etc.) — none of these affect Buildkite CI.
  requires_ci=false
  while IFS= read -r -d '' file; do
    if [[ "$file" =~ ^docs/ ]]; then continue; fi
    if [[ "$file" =~ ^\.github/ ]]; then continue; fi
    if [[ "$file" =~ ^[^/]+\.md$ ]]; then continue; fi
    requires_ci=true
    break
  done < <(git diff -z --name-only "origin/$base_branch...HEAD")

  if [[ "$requires_ci" == "false" ]]; then
    echo "--- Skipping CI as only docs or GHA workflows changed"

    # Extract commit-status contexts from the real testsuite so the dummy
    # pipeline reports a passing status for each one (kept in sync with
    # branch-protection rules automatically). Only match `context:` keys
    # that are children of `github_commit_status:` — other plugins (e.g.
    # junit-annotate) reuse the `context:` key for unrelated values.
    contexts=$(awk '
      /^[[:space:]]*-?[[:space:]]*github_commit_status:[[:space:]]*$/ { expect = 1; next }
      expect && match($0, /^[[:space:]]*context:[[:space:]]*/) {
        print substr($0, RLENGTH + 1)
        expect = 0
      }
    ' "$testsuite" | sort -u)

    {
      echo "steps:"
      echo "  - command: echo \"Skipped CI as no source files changed\""
      echo "    label: \":fast_forward: Skipped\""
      echo "    agents:"
      echo "      queue: devprod-t4gmicro"
      echo "    notify:"
      while IFS= read -r ctx; do
        [[ -z "$ctx" ]] && continue
        echo "      - github_commit_status:"
        echo "          context: \"$ctx\""
      done <<< "$contexts"
    } | buildkite-agent pipeline upload
    exit 0
  fi
fi

# Fallback to normal behavior
buildkite-agent pipeline upload "$testsuite"
