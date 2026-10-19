#!/usr/bin/env bash
# Run a command in every module of the workspace, and report every failure rather than stopping at the first.
#
# Usage: scripts/each-module.sh <command> [args...]
#
# The module list always comes from go.work, so this also works when the caller sets GOWORK=off for the command itself.
# Written for bash 3.2, which macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -eq 0 ]]; then
	echo "usage: $0 <command> [args...]" >&2
	exit 2
fi

dirs="$(cd "$root" && GOWORK="$root/go.work" go list -m -f '{{.Dir}}')"

failed=""
while IFS= read -r dir; do
	name="${dir#"$root"}"
	name="${name#/}"
	name="${name:-.}"
	echo "==> ${name}: $*"
	if ! (cd "$dir" && "$@"); then
		failed="${failed} ${name}"
	fi
done <<<"$dirs"

if [[ -n "$failed" ]]; then
	echo "failed in:${failed}" >&2
	exit 1
fi
