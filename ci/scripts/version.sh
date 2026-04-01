#!/usr/bin/env bash
# vi: ft=sh
#
# This script is responsible for computing the version of a module  from the
# output of `git describe`.
#
# `git describe --dirty` can generate 1 of 4 outputs:
# 1. v0.0.0 - HEAD is tagged as v0.0.0 and no changes are in the index.
# 2. v0.0.0-dirty - HEAD is tagged as v0.0.0 and there are changes in the index.
# 3. v0.0.0-<N>-g<commit> - HEAD is at <commit> which is N commits away from v0.0.0; no changes in index.
# 4. v0.0.0-<N>-g<commit>-dirty - HEAD is at <commit> which is N commits away from v0.0.0; changes in index.
# `--tags` is required to match tags with `/`'s in them which we have due to go modules' tagging conventions.
# `--match` is used to target tags that apply to a specific module.
# `--always` is a fallback to print out the commit if no tag is found.
#
# If a NEXT_VERSION file exists in the module directory (e.g. operator/NEXT_VERSION),
# its contents are used as the version prefix for non-tagged builds instead of the
# version from the nearest ancestor tag. This is useful when release tags live on
# release branches and the nearest ancestor tag on main is outdated.
# The file should contain a single line with the version (e.g. "v25.4.0").

MODULE=""

# --dirty can't be used with a commit-ish in git describe. So rather than
# defaulting to HEAD, we default to --dirty which then implies HEAD.
COMMITISH="--dirty"

while [[ $# -gt 0 ]]; do
	case $1 in
		charts/console|charts/redpanda|charts/operator|operator|gotohelm)
			MODULE="$1"
			shift 1
			;;
		-r|--ref)
			# For testing purposes, allow passing in a commit.
			COMMITISH="$2"
			shift 2
			;;
		-d|--debug|-v|--verbose)
			set -x
			shift
			;;
		-*)
			echo "unhandled argument: $1" 1>&2
			echo "usage: $0 [-v|--verbose] [-r|--ref <COMMITISH>] <go module>" 1>&2
			exit 1
			;;
		*)
			echo "unknown module: $1" 1>&2
			echo "usage: $0 [-v|--verbose] [-r|--ref <COMMITISH>] <go module>" 1>&2
			exit 1
			;;
	esac
done

REPO_ROOT="$(git rev-parse --show-toplevel)"

# Build a pattern to match git tags against. e.g. charts/redpanda/v*
PATTERN="$MODULE"'/v*'
DESC="$(git describe --tags --match "$PATTERN" "$COMMITISH" 2>/dev/null)"
RES="$?"

# If no such tag is found, git describe will exit non-zero and we'll fallback
# to --always which will output a commit and a pseudo version of v0.0.0
if [ $RES -ne 0 ]; then
	DESC="${MODULE}/v0.0.0-$(git describe --always --tags --match "$PATTERN" "$COMMITISH")"
fi

# Trim the full description to just the version. charts/redpanda/v1.2.3 -> v1.2.3
VERSION="${DESC#"$MODULE"/}"

# If HEAD is not exactly on a tag (cases 3 and 4) and a NEXT_VERSION file exists,
# replace the version prefix from git describe with the contents of NEXT_VERSION.
# This handles the case where release tags live on release branches and the nearest
# ancestor tag on main is outdated.
NEXT_VERSION_FILE="${REPO_ROOT}/${MODULE}/NEXT_VERSION"
if [[ "$VERSION" =~ -[0-9]+-g[0-9a-f]+(-dirty)?$ ]] && [ -f "$NEXT_VERSION_FILE" ]; then
	NEXT_VERSION="$(cat "$NEXT_VERSION_FILE" | tr -d '[:space:]')"
	# Extract the describe suffix (-N-gCOMMIT and optionally -dirty)
	DESCRIBE_SUFFIX="$(echo "$VERSION" | sed -E 's/^.*(-[0-9]+-g[0-9a-f]+(-dirty)?)$/\1/')"
	VERSION="${NEXT_VERSION}${DESCRIBE_SUFFIX}"
fi

echo "$VERSION"
