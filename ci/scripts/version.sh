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
echo "${DESC#"$MODULE"/}"
