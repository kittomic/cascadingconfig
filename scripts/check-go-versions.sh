#!/usr/bin/env bash
# Run the tests on the latest Go (with the race detector) and on the minimum Go from the go directive.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

# "go 1.26.0" -> "1.26"
min="$(sed -n 's/^go \([0-9]*\.[0-9]*\).*/\1/p' go.mod)"
if [[ -z "$min" ]]; then
	echo "could not read the go directive from go.mod" >&2
	exit 1
fi

echo "### latest Go: $(go env GOVERSION)"
# -count=1: go test's cache re-checks only files inside the tested module, so a test reading another module's code,
# docs/ or api/ (internal/apicheck, the presets' doc examples) could pass from the cache after they changed (DL-259).
scripts/each-module.sh go test -race -count=1 ./...

echo "### minimum Go: ${min}"
# GOTOOLCHAIN=local stops Go from switching to a newer toolchain, so the minimum is really what runs.
GOTOOLCHAIN=local mise exec "go@${min}" -- scripts/each-module.sh go test ./...
