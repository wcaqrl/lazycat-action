#!/usr/bin/env bash
set -euo pipefail

npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
