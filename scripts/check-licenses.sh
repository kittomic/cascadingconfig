#!/usr/bin/env bash
# Check that every module has a LICENSE identical to the root one. Go packages each module separately, with only that
# module's directory, and pkg.go.dev only shows a module's documentation if it finds a licence there.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "$root/LICENSE" ]]; then
	echo "missing: LICENSE" >&2
	exit 1
fi

bad=""
while IFS= read -r dir; do
	if ! cmp -s "$root/LICENSE" "$dir/LICENSE"; then
		name="${dir#"$root"/}"
		bad="${bad} ${name}"
	fi
done <<<"$(cd "$root" && GOWORK="$root/go.work" go list -m -f '{{.Dir}}')"

if [[ -n "$bad" ]]; then
	echo "LICENSE missing or different from the root copy in:${bad}" >&2
	exit 1
fi
echo "LICENSE present and identical in every module"
