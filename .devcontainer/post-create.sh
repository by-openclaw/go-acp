#!/usr/bin/env bash
# Runs once inside the container right after it is built.
# Installs Go dev tools + project deps. Idempotent — safe to re-run.
set -euo pipefail

echo ">>> Go version"
go version

echo ">>> Installing Go dev tools"
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/go-delve/delve/cmd/dlv@latest

echo ">>> go mod tidy"
cd /workspaces/acp
go mod tidy

echo ">>> go build + vet"
go build ./... || echo "(build failed — expected until acp1 plugin lands)"
go vet ./...  || true

echo ">>> Node version"
node --version
npm --version

echo ">>> Installing Node test-harness deps (any internal/**/test-harness/package.json)"
while IFS= read -r -d '' pkg; do
    dir="$(dirname "$pkg")"
    echo "    npm install in $dir"
    (cd "$dir" && npm install --silent) || echo "    (failed in $dir — continuing)"
done < <(find internal -type f -name package.json -not -path '*/node_modules/*' -print0)

echo ">>> Done."
