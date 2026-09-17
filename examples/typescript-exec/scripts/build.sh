#!/usr/bin/env bash
set -euo pipefail

npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
npx --no-install pkg --targets node22-linux-x64 --output dist/content/app dist/server.js
