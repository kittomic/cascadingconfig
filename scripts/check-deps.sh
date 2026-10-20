#!/usr/bin/env bash
# Check the dependency budget (DL-173): for each setup in docs/project-layout.md whose modules exist, build a throwaway
# consumer module that imports them, and compare the third-party modules it pulls in with the expected counts.
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
)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# module_of maps an imported package to its module in this repository.
module_of() {
	case "$1" in
	"${repo}/format/"*) echo "${repo}/format/$(echo "${1#"${repo}/format/"}" | cut -d/ -f1)" ;;
	*) echo "$repo" ;;
	esac
}

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
		for m in $(for p in $pkgs; do module_of "$p"; done | sort -u); do
			echo "replace $m => $root${m#"$repo"}"
		done
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
	build="$(cd "$dir" && GOWORK=off go list -deps -f '{{with .Module}}{{.Path}}{{end}}' . | third_party | wc -l)"
	graph="$(cd "$dir" && GOWORK=off go list -m all | cut -d' ' -f1 | third_party | wc -l)"
	got="$(echo "$gomod $gosum $build $graph" | tr -s ' ' | sed 's/^ //')"

	if [[ "$got" == "$want" ]]; then
		echo "${name}: go.mod go.sum build graph = ${got}"
	else
		echo "${name}: go.mod go.sum build graph = ${got}, want ${want} (docs/project-layout.md#dependency-analysis)" >&2
		failed="${failed} ${name}"
	fi
done

if [[ -n "$failed" ]]; then
	echo "dependency budget exceeded in:${failed}" >&2
	exit 1
fi
echo "dependency budget met in every setup"
