#!/usr/bin/env bash
set -euo pipefail

cargo build --locked --release --target x86_64-unknown-linux-gnu
rm -rf dist/content
mkdir -p dist/content
cp target/x86_64-unknown-linux-gnu/release/example dist/content/app
