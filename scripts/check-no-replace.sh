#!/usr/bin/env bash
# Check that no released module's go.mod has a replace directive (DL-162, DL-286). A replace only works inside this
# repository: an app that requires the module never sees it, so a released module must build from released versions.
# The modules that are never released, examples and those under internal/, may replace.
# Written for bash 3.2, which macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

bad=""
while IFS= read -r dir; do
	name="${dir#"$root"}"
	name="${name#/}"
	name="${name:-.}"
	case "$name" in
	examples | internal/*) continue ;;
	esac
	if [[ -n "$(cd "$dir" && go mod edit -json | sed -n '/"Replace"/p')" ]]; then
		bad="${bad} ${name}"
	fi
done <<<"$(cd "$root" && GOWORK="$root/go.work" go list -m -f '{{.Dir}}')"

if [[ -n "$bad" ]]; then
	echo "released modules with a replace directive:${bad}" >&2
	exit 1
fi
echo "no released module has a replace directive"
