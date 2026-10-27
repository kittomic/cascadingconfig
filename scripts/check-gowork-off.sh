#!/usr/bin/env bash
# Build and test every module on its own, without go.work. This catches a module that silently depends on unreleased
# changes in another module of this repository.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

GOWORK=off scripts/each-module.sh go test ./...
