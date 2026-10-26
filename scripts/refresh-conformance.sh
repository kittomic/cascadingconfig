#!/usr/bin/env bash
# Replace a conformance suite's vendored files with those at its pinned version (DL-269). Run by hand, never from the
# hooks, since it uses the network. Change a pin here, run the script, and commit the result with the outcomes it
# changes.
#
# Usage: scripts/refresh-conformance.sh toml|yaml|json
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="$root/internal/conformance/testdata"

toml_repo=toml-lang/toml-test toml_ref=v2.2.0
yaml_repo=yaml/yaml-test-suite yaml_ref=data-2022-01-17 yaml_license_ref=v2022-01-17
json_repo=nst/JSONTestSuite json_ref=1ef36fa01286573e846ac449e8683f8833c5b26a

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# fetch downloads repo at ref and extracts it into $work/$3.
fetch() {
	mkdir -p "$work/$3"
	curl -fsSL "https://codeload.github.com/$1/tar.gz/$2" | tar -xz -C "$work/$3" --strip-components=1
}

# vendor replaces $data/$1 with the files listed on stdin, relative to $work/$2, plus a SOURCE file.
vendor() {
	local dest="$data/$1" src="$work/$2" repo="$3" ref="$4"
	rm -rf "$dest"
	mkdir -p "$dest"
	while IFS= read -r f; do
		mkdir -p "$dest/$(dirname "$f")"
		cp "$src/$f" "$dest/$f"
	done
	printf 'https://github.com/%s\n%s\n' "$repo" "$ref" >"$dest/SOURCE"
}

case "${1:-}" in
toml)
	fetch "$toml_repo" "$toml_ref" toml
	{
		echo LICENSE
		echo tests/files-toml-1.1.0
		sed 's|^|tests/|' "$work/toml/tests/files-toml-1.1.0"
	} | vendor toml-test toml "$toml_repo" "$toml_ref"
	;;
yaml)
	fetch "$yaml_repo" "$yaml_ref" yaml
	fetch "$yaml_repo" "$yaml_license_ref" yaml-license
	cp "$work/yaml-license/LICENSE" "$work/yaml/LICENSE"
	(cd "$work/yaml" && { echo LICENSE; find . -type f \( -name in.yaml -o -name in.json -o -name error -o -name '===' \) | sed 's|^\./||' | sort; }) |
		vendor yaml-test-suite yaml "$yaml_repo" "$yaml_ref"
	;;
json)
	fetch "$json_repo" "$json_ref" json
	(cd "$work/json" && { echo LICENSE; find test_parsing -type f -name '*.json' | sort; }) |
		vendor JSONTestSuite json "$json_repo" "$json_ref"
	;;
*)
	echo "usage: $0 toml|yaml|json" >&2
	exit 2
	;;
esac
echo "refreshed $1 into internal/conformance/testdata"
