#!/usr/bin/env bash
# Check the dependency budget (DL-173): for each setup in docs/contributing/project-layout.md whose modules exist, build
# a throwaway consumer module that imports them, and compare the third-party modules it pulls in with the expected
# counts. Every third-party module a consumer builds with must also be in the licence list below (DL-280).
#
# The consumer replaces this repository's modules with the local copies, so it measures what an app would get from the
# current code. go mod tidy needs the module cache or network access. Written for bash 3.2, which macOS ships.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repo="github.com/kittomic/cascadingconfig"

# Each setup: a name, the expected counts (go.mod go.sum build graph), and the packages its consumer imports.
# Later milestones add setups as their modules gain dependencies.
setups=(
	"core-only|1 1 1 1|${repo}"
	"minimal|2 2 2 2|${repo} ${repo}/format/toml"
	"core-toml-yaml|3 3 3 3|${repo} ${repo}/format/toml ${repo}/format/yaml"
	"core-oci|5 5 5 7|${repo} ${repo}/ociresolver"
	"core-fsnotify|3 3 3 3|${repo} ${repo}/watch/fsnotify"
	"presets-mise|9 9 9 11|${repo}/presets/mise"
	"presets-misehardened|5 5 5 9|${repo}/presets/misehardened"
	"presets-cosmiconfig|4 4 4 9|${repo}/presets/cosmiconfig"
)

# Each third-party module the released modules build with, and its licence, as reviewed in
# docs/design/decisions/0005-dependencies-before-release.md. A new dependency fails the check until its licence is
# reviewed, found compatible with Apache-2.0, and listed here.
licences=(
	"github.com/fsnotify/fsnotify|BSD-3-Clause"
	"github.com/go-viper/mapstructure/v2|MIT"
	"github.com/goccy/go-yaml|MIT"
	"github.com/opencontainers/go-digest|Apache-2.0"
	"github.com/opencontainers/image-spec|Apache-2.0"
	"github.com/pelletier/go-toml/v2|MIT"
	"golang.org/x/sync|BSD-3-Clause"
	"golang.org/x/sys|BSD-3-Clause"
	"oras.land/oras-go/v2|Apache-2.0"
)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# modules lists every module in this repository, as "path dir", for the consumers' replace directives: a module
# that requires its siblings, such as presets, needs them replaced in the consumer too, and an unused replace changes no
# count.
modules="$(cd "$root" && GOWORK="$root/go.work" go list -m -f '{{.Path}} {{.Dir}}')"

# third_party filters out this repository's modules and the consumer itself.
third_party() {
	grep -v -e "^${repo}\$" -e "^${repo}/" -e '^consumer$' | sort -u | sed '/^$/d'
}

failed=""
for setup in "${setups[@]}"; do
	name="${setup%%|*}"
	rest="${setup#*|}"
	want="${rest%%|*}"
	pkgs="${rest#*|}"

	dir="$work/$name"
	mkdir -p "$dir"
	{
		echo "module consumer"
		echo
		echo "go 1.26.0"
		echo
		while read -r path moddir; do
			echo "replace $path => $moddir"
		done <<<"$modules"
	} >"$dir/go.mod"
	{
		echo "package main"
		echo
		echo "import ("
		for p in $pkgs; do
			echo "	_ \"$p\""
		done
		echo ")"
		echo
		echo "func main() {}"
	} >"$dir/main.go"

	(cd "$dir" && GOWORK=off GOFLAGS=-mod=mod go mod tidy >/dev/null 2>&1) || {
		echo "${name}: go mod tidy failed" >&2
		(cd "$dir" && GOWORK=off go mod tidy) >&2 || true
		failed="${failed} ${name}"
		continue
	}

	gomod="$(awk '/^require \(/{r=1; next} r && /^\)/{r=0} r{print $1} /^require [^(]/{print $2}' "$dir/go.mod" | third_party | wc -l)"
	gosum="$( (cut -d' ' -f1 "$dir/go.sum" 2>/dev/null || true) | third_party | wc -l)"
	built="$(cd "$dir" && GOWORK=off go list -deps -f '{{with .Module}}{{.Path}}{{end}}' . | third_party)"
	build="$(echo "$built" | sed '/^$/d' | wc -l)"
	graph="$(cd "$dir" && GOWORK=off go list -m all | cut -d' ' -f1 | third_party | wc -l)"
	got="$(echo "$gomod $gosum $build $graph" | tr -s ' ' | sed 's/^ //')"

	if [[ "$got" == "$want" ]]; then
		echo "${name}: go.mod go.sum build graph = ${got}"
	else
		echo "${name}: go.mod go.sum build graph = ${got}, want ${want} (docs/contributing/project-layout.md#dependency-analysis)" >&2
		failed="${failed} ${name}"
	fi

	while IFS= read -r mod; do
		[[ -z "$mod" ]] && continue
		listed=""
		for l in "${licences[@]}"; do
			[[ "${l%%|*}" == "$mod" ]] && listed=1
		done
		if [[ -z "$listed" ]]; then
			echo "${name}: ${mod} isn't in the licence list in scripts/check-deps.sh; review its licence first (DL-280)" >&2
			failed="${failed} ${name}"
		fi
	done <<<"$built"
done

if [[ -n "$failed" ]]; then
	echo "dependency budget or licence list failed in:${failed}" >&2
	exit 1
fi
echo "dependency budget met, and every dependency's licence listed, in every setup"
