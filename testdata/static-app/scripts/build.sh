#!/usr/bin/env bash
set -euo pipefail

test "${LAZYCAT_TARGET_OS}" = "linux"
test "${LAZYCAT_TARGET_ARCH}" = "amd64"
test "${LAZYCAT_TARGET_PLATFORM}" = "linux/amd64"
