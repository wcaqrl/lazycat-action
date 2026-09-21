#!/usr/bin/env bash
set -euo pipefail

config="${LAZYCAT_CONFIG:-lazycat-action.yml}"
operation="${LAZYCAT_OPERATION:-auto}"

args=(
  run
  --operation "${operation}"
  --config "${config}"
)

if [[ -n "${LAZYCAT_VERSION:-}" ]]; then
  args+=(--version "${LAZYCAT_VERSION}")
fi
if [[ -n "${LAZYCAT_CHANGELOG:-}" ]]; then
  args+=(--changelog "${LAZYCAT_CHANGELOG}")
fi
if [[ "${LAZYCAT_DRY_RUN:-false}" == "true" ]]; then
  args+=(--dry-run)
fi
if [[ "${LAZYCAT_PUBLISH_AFTER_CHECK:-true}" == "true" ]]; then
  args+=(--publish-after-check)
fi

exec "$(dirname "$0")/run-action.sh" "${args[@]}"
