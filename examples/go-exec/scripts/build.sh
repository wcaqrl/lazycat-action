#!/usr/bin/env bash
set -euo pipefail

rm -rf dist/content
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
