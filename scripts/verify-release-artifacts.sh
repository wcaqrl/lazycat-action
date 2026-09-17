#!/usr/bin/env bash
set -euo pipefail

dist="${1:-dist}"

(cd "${dist}" && sha256sum --check checksums.txt)

for arch in amd64 arm64; do
  archive="${dist}/lazycat-action_linux_${arch}.tar.gz"
  entries="$(tar -tzf "${archive}")"
  if [[ "${entries}" != "lazycat-action" ]]; then
    echo "${archive} must contain only lazycat-action" >&2
    exit 1
  fi
  entry_type="$(tar -tvzf "${archive}" | cut -c1)"
  if [[ "${entry_type}" != "-" ]]; then
    echo "${archive} does not contain a regular binary" >&2
    exit 1
  fi
done
